package engine

import (
	"fmt"
	"io"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/exec"
	"github.com/vatsalpatel/sqlette/internal/pager"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/planner"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type Result struct {
	Columns []string
	Rows    [][]values.Value
	Message string
}

type Engine struct {
	cat   *catalog.Catalog
	store *storage.Store
	inTxn bool
}

func New() (*Engine, error) {
	return Open("")
}

func Open(path string) (*Engine, error) {
	store, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	e := &Engine{store: store}
	if err := e.reload(); err != nil {
		return nil, err
	}
	return e, nil
}

// reload rebuilds the in-memory catalog and table map from the schema on disk.
// It is both the open path and the undo for in-memory state after a rollback.
func (e *Engine) reload() error {
	blob, err := e.store.ReadSchema()
	if err != nil {
		return err
	}
	e.cat = catalog.New()
	if err := e.cat.Unmarshal(blob); err != nil {
		return err
	}
	for name, tbl := range e.cat.Tables {
		if err := e.store.AttachTable(name, tbl.RootPage); err != nil {
			return err
		}
	}
	for name, ix := range e.cat.Indexes {
		ctbl, ok := e.cat.Get(ix.Table)
		if !ok {
			return fmt.Errorf("index %s references unknown table %s", name, ix.Table)
		}
		stbl, ok := e.store.Table(ix.Table)
		if !ok {
			return fmt.Errorf("index %s references unknown table %s", name, ix.Table)
		}
		cols, err := columnPositions(ctbl, ix.Columns)
		if err != nil {
			return err
		}
		attached, err := e.store.AttachIndex(name, ix.RootPage, cols, ix.Unique)
		if err != nil {
			return err
		}
		stbl.AddIndex(attached)
	}
	return nil
}

func (e *Engine) Close() error {
	if e.inTxn {
		if err := e.store.Rollback(); err != nil {
			return err
		}
		e.inTxn = false
	}
	return e.store.Close()
}

func (e *Engine) Exec(stmt ast.Statement) (*Result, error) {
	switch stmt.(type) {
	case *ast.BeginStmt:
		return e.begin()
	case *ast.CommitStmt:
		return e.commit()
	case *ast.RollbackStmt:
		return e.rollback()
	}

	if e.inTxn {
		return e.dispatch(stmt) // inside an explicit transaction; COMMIT will flush
	}

	// autocommit: wrap the statement in its own transaction
	if err := e.store.Begin(); err != nil {
		return nil, err
	}
	res, err := e.dispatch(stmt)
	if err != nil {
		if rbErr := e.rollbackState(); rbErr != nil {
			return nil, rbErr
		}
		return nil, err
	}
	if err := e.store.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func (e *Engine) dispatch(stmt ast.Statement) (*Result, error) {
	switch st := stmt.(type) {
	case *ast.CreateStmt:
		return e.execCreate(st)
	case *ast.CreateIndexStmt:
		return e.execCreateIndex(st)
	case *ast.InsertStmt:
		return e.execInsert(st)
	case *ast.UpdateStmt:
		return e.execUpdate(st)
	case *ast.DeleteStmt:
		return e.execDelete(st)
	case *ast.SelectStmt:
		return e.execSelect(st)
	case *ast.ExplainStmt:
		return e.execExplain(st)
	default:
		return nil, fmt.Errorf("unknown statement type %T", stmt)
	}
}

func (e *Engine) begin() (*Result, error) {
	if e.inTxn {
		return nil, fmt.Errorf("cannot start a transaction within a transaction")
	}
	if err := e.store.Begin(); err != nil {
		return nil, err
	}
	e.inTxn = true
	return &Result{Message: "ok"}, nil
}

func (e *Engine) commit() (*Result, error) {
	if !e.inTxn {
		return nil, fmt.Errorf("no transaction is active")
	}
	if err := e.store.Commit(); err != nil {
		return nil, err
	}
	e.inTxn = false
	return &Result{Message: "ok"}, nil
}

func (e *Engine) rollback() (*Result, error) {
	if !e.inTxn {
		return nil, fmt.Errorf("no transaction is active")
	}
	if err := e.rollbackState(); err != nil {
		return nil, err
	}
	e.inTxn = false
	return &Result{Message: "ok"}, nil
}

// rollbackState undoes both halves of a transaction: the pager restores the
// pages, then reload() rebuilds the catalog and table map from them.
func (e *Engine) rollbackState() error {
	if err := e.store.Rollback(); err != nil {
		return err
	}
	return e.reload()
}

func (e *Engine) execCreate(stmt *ast.CreateStmt) (*Result, error) {
	cols := make([]catalog.Column, len(stmt.Columns))
	for i, col := range stmt.Columns {
		cols[i] = catalog.Column{Name: col.Name, Type: col.Type, PrimaryKey: col.PrimaryKey, NotNull: col.NotNull}
	}
	ctbl := &catalog.Table{
		Name:     strings.ToLower(stmt.Table),
		Columns:  cols,
		RootPage: pager.PageID(0),
	}
	if err := e.cat.Create(ctbl); err != nil {
		return nil, err
	}

	stbl, err := e.store.CreateTable(stmt.Table)
	if err != nil {
		return nil, err
	}
	ctbl.RootPage = stbl.Root()

	if err := e.store.WriteSchema(e.cat.Marshal()); err != nil {
		return nil, err
	}
	return &Result{Message: "ok"}, nil
}

func (e *Engine) execInsert(stmt *ast.InsertStmt) (*Result, error) {
	ctbl, ok := e.cat.Get(stmt.Table)
	if !ok || ctbl == nil {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}
	stbl, ok := e.store.Table(stmt.Table)
	if !ok || stbl == nil {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}

	for _, exprs := range stmt.Rows {
		if len(exprs) != len(ctbl.Columns) {
			return nil, fmt.Errorf("row has %d columns, table has %d", len(exprs), len(ctbl.Columns))
		}
		row := make([]values.Value, len(exprs))
		for i, v := range exprs {
			val, err := exec.EvalConst(v)
			if err != nil {
				return nil, err
			}
			row[i] = val
		}
		if _, err := stbl.Insert(row); err != nil {
			return nil, err
		}
	}

	return &Result{Message: fmt.Sprintf("%d rows inserted", len(stmt.Rows))}, nil
}

func (e *Engine) execUpdate(stmt *ast.UpdateStmt) (*Result, error) {
	schema, ok := e.cat.Get(stmt.Table)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}
	table, ok := e.store.Table(stmt.Table)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}

	indices := make([]int, len(stmt.Assigns))
	for i, a := range stmt.Assigns {
		idx, ok := schema.ColumnIndex(a.Column)
		if !ok {
			return nil, fmt.Errorf("column %s not found", a.Column)
		}
		indices[i] = idx
	}

	node := planner.ScanTable(e.cat, schema, "", stmt.Where)
	op, scope, err := exec.Build(node, e.store)
	if err != nil {
		return nil, err
	}
	scanner, ok := exec.Scanner(op)
	if !ok {
		return nil, fmt.Errorf("expected a row scanner, got %T", op)
	}
	if err := scanner.Open(); err != nil {
		return nil, err
	}

	type pending struct {
		id  int64
		row []values.Value
	}
	var updates []pending
	for {
		row, err := scanner.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		newRow := make([]values.Value, len(row))
		copy(newRow, row)
		for i, a := range stmt.Assigns {
			val, err := exec.Eval(a.Value, row, scope)
			if err != nil {
				return nil, err
			}
			newRow[indices[i]] = val
		}
		updates = append(updates, pending{id: scanner.RowID(), row: newRow})
	}
	if err := scanner.Close(); err != nil {
		return nil, err
	}

	for _, u := range updates {
		if _, err := table.Update(u.id, u.row); err != nil {
			return nil, err
		}
	}

	return &Result{Message: fmt.Sprintf("%d rows updated", len(updates))}, nil
}

func (e *Engine) execDelete(stmt *ast.DeleteStmt) (*Result, error) {
	schema, ok := e.cat.Get(stmt.Table)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}
	table, ok := e.store.Table(stmt.Table)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}

	node := planner.ScanTable(e.cat, schema, "", stmt.Where)
	op, _, err := exec.Build(node, e.store)
	if err != nil {
		return nil, err
	}
	scanner, ok := exec.Scanner(op)
	if !ok {
		return nil, fmt.Errorf("expected a row scanner, got %T", op)
	}
	if err := scanner.Open(); err != nil {
		return nil, err
	}
	var ids []int64
	for {
		_, err := scanner.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		ids = append(ids, scanner.RowID())
	}
	if err := scanner.Close(); err != nil {
		return nil, err
	}

	for _, id := range ids {
		if _, err := table.Delete(id); err != nil {
			return nil, err
		}
	}

	return &Result{Message: fmt.Sprintf("%d rows deleted", len(ids))}, nil
}

func (e *Engine) execSelect(stmt *ast.SelectStmt) (*Result, error) {
	node, err := planner.Select(e.cat, stmt)
	if err != nil {
		return nil, err
	}
	op, scope, err := exec.Build(node, e.store)
	if err != nil {
		return nil, err
	}

	if err := op.Open(); err != nil {
		return nil, err
	}
	defer op.Close()

	rows := [][]values.Value{}
	for {
		row, err := op.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		rows = append(rows, row)
	}
	return &Result{Columns: columnNames(scope), Rows: rows}, nil
}

func (e *Engine) execCreateIndex(stmt *ast.CreateIndexStmt) (*Result, error) {
	stmt.Name = strings.ToLower(stmt.Name)

	ctbl, ok := e.cat.Get(stmt.Table)
	if !ok || ctbl == nil {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}
	if _, ok := e.cat.GetIndex(stmt.Name); ok {
		return nil, fmt.Errorf("index %s already exists", stmt.Name)
	}
	cols, err := columnPositions(ctbl, stmt.Columns)
	if err != nil {
		return nil, err
	}
	stbl, ok := e.store.Table(stmt.Table)
	if !ok || stbl == nil {
		return nil, fmt.Errorf("table %s does not exist", stmt.Table)
	}

	ix, err := e.store.CreateIndex(stmt.Name, cols, stmt.Unique)
	if err != nil {
		return nil, err
	}
	if err := stbl.BuildIndex(ix); err != nil {
		return nil, err
	}

	if err := e.cat.CreateIndex(&catalog.Index{
		Name:     stmt.Name,
		Table:    stmt.Table,
		Columns:  stmt.Columns,
		Unique:   stmt.Unique,
		RootPage: ix.Root(),
	}); err != nil {
		return nil, err
	}
	if err := e.store.WriteSchema(e.cat.Marshal()); err != nil {
		return nil, err
	}
	return &Result{Message: "ok"}, nil
}

func columnPositions(ctbl *catalog.Table, names []string) ([]int, error) {
	cols := make([]int, len(names))
	for i, name := range names {
		idx, ok := ctbl.ColumnIndex(name)
		if !ok {
			return nil, fmt.Errorf("column %s not found in table %s", name, ctbl.Name)
		}
		cols[i] = idx
	}
	return cols, nil
}

func (e *Engine) execExplain(stmt *ast.ExplainStmt) (*Result, error) {
	node, err := e.explainPlan(stmt.Stmt)
	if err != nil {
		return nil, err
	}
	return &Result{Message: buildPlan(node, 0)}, nil
}

func (e *Engine) explainPlan(stmt ast.Statement) (plan.Node, error) {
	switch st := stmt.(type) {
	case *ast.SelectStmt:
		return planner.Select(e.cat, st)
	case *ast.DeleteStmt:
		schema, ok := e.cat.Get(st.Table)
		if !ok {
			return nil, fmt.Errorf("table %s does not exist", st.Table)
		}
		return &plan.Delete{Input: planner.ScanTable(e.cat, schema, "", st.Where), Table: schema.Name}, nil
	case *ast.UpdateStmt:
		schema, ok := e.cat.Get(st.Table)
		if !ok {
			return nil, fmt.Errorf("table %s does not exist", st.Table)
		}
		return &plan.Update{Input: planner.ScanTable(e.cat, schema, "", st.Where), Table: schema.Name}, nil
	default:
		return nil, fmt.Errorf("cannot explain %T", stmt)
	}
}

func buildPlan(node plan.Node, depth int) string {
	indent := strings.Repeat("  ", depth)
	switch n := node.(type) {
	case *plan.SeqScan:
		return fmt.Sprintf("%s(seqscan %s)", indent, n.Table)
	case *plan.Filter:
		return fmt.Sprintf("%s(filter %s)\n", indent, n.Predicate) + buildPlan(n.Input, depth+1)
	case *plan.Project:
		cols := make([]string, len(n.Columns))
		for i, c := range n.Columns {
			cols[i] = c.String()
		}
		return fmt.Sprintf("%s(project %s)\n", indent, strings.Join(cols, " ")) + buildPlan(n.Input, depth+1)
	case *plan.IndexScan:
		return fmt.Sprintf("%s(indexscan %s using %s%s)", indent, n.Table, n.Index, indexBounds(n))
	case *plan.OneRow:
		return fmt.Sprintf("%s(onerow)", indent)
	case *plan.Sort:
		terms := make([]string, len(n.Keys))
		for i, k := range n.Keys {
			dir := "asc"
			if k.Desc {
				dir = "desc"
			}
			terms[i] = fmt.Sprintf("(%s %v)", dir, k.Expr)
		}
		return fmt.Sprintf("%s(sort %s)\n", indent, strings.Join(terms, " ")) + buildPlan(n.Input, depth+1)
	case *plan.Limit:
		out := fmt.Sprintf("%s(limit %d", indent, n.Count)
		if n.Offset > 0 {
			out += fmt.Sprintf(" offset %d", n.Offset)
		}
		return out + ")\n" + buildPlan(n.Input, depth+1)
	case *plan.Delete:
		return fmt.Sprintf("%s(delete %s)\n", indent, n.Table) + buildPlan(n.Input, depth+1)
	case *plan.Update:
		return fmt.Sprintf("%s(update %s)\n", indent, n.Table) + buildPlan(n.Input, depth+1)
	default:
		return fmt.Sprintf("%s(unknown %T)", indent, node)
	}
}

func indexBounds(n *plan.IndexScan) string {
	if n.Low != nil && n.High != nil &&
		n.Low.Inclusive && n.High.Inclusive && n.Low.Value == n.High.Value {
		return fmt.Sprintf(" (= %s %v)", n.Column, n.Low.Value)
	}
	var out string
	if n.Low != nil {
		op := ">"
		if n.Low.Inclusive {
			op = ">="
		}
		out += fmt.Sprintf(" (%s %s %v)", op, n.Column, n.Low.Value)
	}
	if n.High != nil {
		op := "<"
		if n.High.Inclusive {
			op = "<="
		}
		out += fmt.Sprintf(" (%s %s %v)", op, n.Column, n.High.Value)
	}
	return out
}

func columnNames(s exec.Scope) []string {
	names := make([]string, len(s))
	for i, c := range s {
		names[i] = c.Name
	}
	return names
}
