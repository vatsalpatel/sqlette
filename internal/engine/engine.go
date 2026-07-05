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
}

func New() (*Engine, error) {
	return Open("")
}

func Open(path string) (*Engine, error) {
	store, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	cat := catalog.New()
	blob, err := store.ReadSchema()
	if err != nil {
		return nil, err
	}
	if err := cat.Unmarshal(blob); err != nil {
		return nil, err
	}
	for name, tbl := range cat.Tables {
		if err := store.AttachTable(name, tbl.RootPage); err != nil {
			return nil, err
		}
	}
	return &Engine{cat: cat, store: store}, nil
}

func (e Engine) Close() error {
	for name, ctbl := range e.cat.Tables {
		if stbl, ok := e.store.Table(name); ok {
			ctbl.RootPage = stbl.Root()
		}
	}
	if err := e.store.WriteSchema(e.cat.Marshal()); err != nil {
		return err
	}
	return e.store.Close()
}

func (e *Engine) Exec(stmt ast.Statement) (*Result, error) {
	switch st := stmt.(type) {
	case *ast.CreateStmt:
		return e.execCreate(st)
	case *ast.InsertStmt:
		return e.execInsert(st)
	case *ast.SelectStmt:
		return e.execSelect(st)
	case *ast.ExplainStmt:
		return e.execExplain(st)
	default:
		return nil, fmt.Errorf("unknown statement type %T", stmt)
	}
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

func (e *Engine) execSelect(stmt *ast.SelectStmt) (*Result, error) {
	schema, ok := e.cat.Get(stmt.From.Name)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", stmt.From.Name)
	}

	var node plan.Node = &plan.SeqScan{Table: stmt.From.Name}
	if stmt.Where != nil {
		node = &plan.Filter{Input: node, Predicate: stmt.Where}
	}

	node = &plan.Project{Input: node, Columns: stmt.Columns}
	op, cols, err := exec.Build(node, e.store, schema)
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
	return &Result{Columns: cols, Rows: rows}, nil
}

func (e *Engine) execExplain(stmt *ast.ExplainStmt) (*Result, error) {
	selectStmt, ok := stmt.Stmt.(*ast.SelectStmt)
	if !ok {
		return nil, fmt.Errorf("expected a SELECT statement, got %T", stmt.Stmt)
	}

	table, ok := e.cat.Get(selectStmt.From.Name)
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", selectStmt.From.Name)
	}
	var node plan.Node = &plan.SeqScan{Table: table.Name}
	if selectStmt.Where != nil {
		node = &plan.Filter{Input: node, Predicate: selectStmt.Where}
	}
	node = &plan.Project{Input: node, Columns: selectStmt.Columns}
	fmt.Println(buildPlan(node, 0))

	return &Result{}, nil
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
	default:
		return fmt.Sprintf("%s(unknown %T)", indent, node)
	}
}
