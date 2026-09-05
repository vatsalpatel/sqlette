package exec

import (
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/token"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type Operator interface {
	Open() error
	Next() (storage.Row, error)
	Close() error
}
type RowScanner interface {
	Operator
	RowID() int64
}

func Scanner(op Operator) (RowScanner, bool) {
	switch o := op.(type) {
	case *seqScan:
		return o, true
	case *indexScan:
		return o, true
	case *filter:
		if o.scanner != nil {
			return o, true
		}
	}
	return nil, false
}

type seqScan struct {
	table  *storage.Table
	cursor storage.Cursor
}

func (s *seqScan) Open() error {
	s.cursor = s.table.Scan()
	return nil
}

func (s *seqScan) Next() (storage.Row, error) {
	if s.cursor.Next() {
		return s.cursor.Row(), nil
	}
	return nil, io.EOF
}

func (s *seqScan) RowID() int64 {
	return s.cursor.RowID()
}

func (s *seqScan) Close() error {
	return s.cursor.Close()
}

type project struct {
	input Operator
	exprs []ast.Expression
	scope Scope
}

func (p *project) Open() error {
	return p.input.Open()
}

func (p *project) Next() (storage.Row, error) {
	row, err := p.input.Next()
	if err != nil {
		return nil, err
	}
	out := make(storage.Row, len(p.exprs))
	for i, expr := range p.exprs {
		v, err := Eval(expr, row, p.scope)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (p *project) Close() error {
	return p.input.Close()
}

type filter struct {
	input   Operator
	scanner RowScanner
	pred    ast.Expression
	scope   Scope
}

func (f *filter) Open() error {
	return f.input.Open()
}

func (f *filter) Next() (storage.Row, error) {
	for {
		row, err := f.input.Next()
		if err != nil {
			return nil, err
		}
		v, err := Eval(f.pred, row, f.scope)
		if err != nil {
			return nil, err
		}
		if truth(v) == 1 {
			return row, nil
		}
	}
}

func (f *filter) RowID() int64 {
	return f.scanner.RowID()
}

func (f *filter) Close() error {
	return f.input.Close()
}

type indexScan struct {
	table     *storage.Table
	ix        *storage.Index
	low, high *storage.Bound
	cursor    storage.Cursor
}

func (i *indexScan) Open() error {
	i.cursor = i.table.IndexScan(i.ix, i.low, i.high)
	return nil
}

func (i *indexScan) Next() (storage.Row, error) {
	if i.cursor.Next() {
		return i.cursor.Row(), nil
	}
	if i.cursor.Err() != nil {
		return nil, i.cursor.Err()
	}
	return nil, io.EOF
}

func (i *indexScan) RowID() int64 { return i.cursor.RowID() }

func (i *indexScan) Close() error { return i.cursor.Close() }

type oneRow struct {
	done bool
}

func (o *oneRow) Open() error { o.done = false; return nil }

func (o *oneRow) Next() (storage.Row, error) {
	if o.done {
		return nil, io.EOF
	}
	o.done = true
	return storage.Row{}, nil
}

func (o *oneRow) Close() error { return nil }

type sortRow struct {
	row  storage.Row
	keys []values.Value
}

type sortKey struct {
	expr ast.Expression
	desc bool
}

type sortOp struct {
	input Operator
	keys  []sortKey
	scope Scope
	rows  []sortRow
	idx   int
}

func (s *sortOp) Open() error {
	if err := s.input.Open(); err != nil {
		return err
	}
	s.rows, s.idx = nil, 0
	for {
		row, err := s.input.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		keys := make([]values.Value, len(s.keys))
		for i, key := range s.keys {
			v, err := Eval(key.expr, row, s.scope)
			if err != nil {
				return err
			}
			keys[i] = v
		}
		s.rows = append(s.rows, sortRow{row: row, keys: keys})
	}
	slices.SortStableFunc(s.rows, func(a, b sortRow) int {
		for i, key := range s.keys {
			if c := values.Compare(a.keys[i], b.keys[i]); c != 0 {
				if key.desc {
					return -c
				}
				return c
			}
		}
		return 0
	})
	return nil
}

func (s *sortOp) Next() (storage.Row, error) {
	if s.idx >= len(s.rows) {
		return nil, io.EOF
	}
	out := s.rows[s.idx].row
	s.idx++
	return out, nil
}

func (s *sortOp) Close() error { return s.input.Close() }

type limitOp struct {
	input   Operator
	count   int64
	offset  int64
	seen    int64
	skipped int64
}

func (l *limitOp) Open() error {
	l.seen, l.skipped = 0, 0
	return l.input.Open()
}

func (l *limitOp) Next() (storage.Row, error) {
	for l.skipped < l.offset {
		if _, err := l.input.Next(); err != nil {
			return nil, err
		}
		l.skipped++
	}
	if l.count >= 0 && l.seen >= l.count {
		return nil, io.EOF
	}
	row, err := l.input.Next()
	if err != nil {
		return nil, err
	}
	l.seen++
	return row, nil
}

func (l *limitOp) Close() error { return l.input.Close() }

func evalBound(b *plan.Bound) (*storage.Bound, error) {
	if b == nil {
		return nil, nil
	}
	v, err := EvalConst(b.Value)
	if err != nil {
		return nil, err
	}
	return &storage.Bound{Value: v, Inclusive: b.Inclusive}, nil
}

func Build(node plan.Node, store *storage.Store) (Operator, Scope, error) {
	switch n := node.(type) {
	case *plan.SeqScan:
		tbl, ok := store.Table(n.Table)
		if !ok {
			return nil, nil, fmt.Errorf("table %s not found", n.Table)
		}
		return &seqScan{table: tbl}, scanScope(n.Table, n.Alias, n.Columns), nil
	case *plan.Project:
		child, scope, err := Build(n.Input, store)
		if err != nil {
			return nil, nil, err
		}
		exprs, out, err := bindColumns(n.Columns, scope)
		if err != nil {
			return nil, nil, err
		}
		return &project{input: child, exprs: exprs, scope: scope}, out, nil
	case *plan.Filter:
		child, scope, err := Build(n.Input, store)
		if err != nil {
			return nil, nil, err
		}
		if err := validate(n.Predicate, scope); err != nil {
			return nil, nil, err
		}
		f := &filter{input: child, pred: n.Predicate, scope: scope}
		f.scanner, _ = Scanner(child)
		return f, scope, nil
	case *plan.IndexScan:
		tbl, ok := store.Table(n.Table)
		if !ok {
			return nil, nil, fmt.Errorf("table %s not found", n.Table)
		}
		ix, ok := store.Index(n.Index)
		if !ok {
			return nil, nil, fmt.Errorf("index %s not found", n.Index)
		}
		low, err := evalBound(n.Low)
		if err != nil {
			return nil, nil, err
		}
		high, err := evalBound(n.High)
		if err != nil {
			return nil, nil, err
		}
		return &indexScan{table: tbl, ix: ix, low: low, high: high}, scanScope(n.Table, n.Alias, n.Columns), nil
	case *plan.OneRow:
		return &oneRow{}, Scope{}, nil
	case *plan.Sort:
		child, scope, err := Build(n.Input, store)
		if err != nil {
			return nil, nil, err
		}
		keys := make([]sortKey, len(n.Keys))
		for i, key := range n.Keys {
			if err := validate(key.Expr, scope); err != nil {
				return nil, nil, err
			}
			keys[i] = sortKey{expr: key.Expr, desc: key.Desc}
		}
		return &sortOp{input: child, keys: keys, scope: scope}, scope, nil
	case *plan.Limit:
		child, scope, err := Build(n.Input, store)
		if err != nil {
			return nil, nil, err
		}
		return &limitOp{input: child, count: n.Count, offset: n.Offset}, scope, nil
	default:
		return nil, nil, fmt.Errorf("unknown plan node %T", node)
	}
}

func bindColumns(cols []ast.ResultColumn, in Scope) ([]ast.Expression, Scope, error) {
	var exprs []ast.Expression
	var out Scope
	for _, col := range cols {
		switch expr := col.Expr.(type) {
		case *ast.Star:
			idxs, err := in.Expand(expr)
			if err != nil {
				return nil, nil, err
			}
			for _, idx := range idxs {
				exprs = append(exprs, &ast.ColumnRef{Table: in[idx].Table, Name: in[idx].Name})
				out = append(out, in[idx])
			}
		case *ast.ColumnRef:
			i, err := in.Resolve(expr)
			if err != nil {
				return nil, nil, err
			}
			exprs = append(exprs, expr)
			if col.Alias != "" {
				out = append(out, Column{Name: col.Alias})
			} else {
				out = append(out, in[i])
			}
		default:
			if err := validate(col.Expr, in); err != nil {
				return nil, nil, err
			}
			exprs = append(exprs, col.Expr)
			name := col.Alias
			if name == "" {
				name = fmt.Sprintf("%v", col.Expr)
			}
			out = append(out, Column{Name: name})
		}
	}
	return exprs, out, nil
}

func Eval(pred ast.Expression, row storage.Row, scope Scope) (values.Value, error) {
	switch pred := pred.(type) {
	case *ast.Literal:
		return EvalConst(pred)
	case *ast.ColumnRef:
		idx, err := scope.Resolve(pred)
		if err != nil {
			return values.NewNull(), err
		}
		return row[idx], nil
	case *ast.Unary:
		switch pred.Op {
		case token.NOT:
			op, err := Eval(pred.Operand, row, scope)
			if err != nil {
				return values.NewNull(), err
			}
			return not3(op), nil
		default:
			return values.NewNull(), fmt.Errorf("unknown unary operator %s", pred.Op)
		}
	case *ast.Binary:
		l, err := Eval(pred.Left, row, scope)
		if err != nil {
			return values.NewNull(), err
		}
		r, err := Eval(pred.Right, row, scope)
		if err != nil {
			return values.NewNull(), err
		}
		switch pred.Op {
		case token.EQ, token.NEQ, token.LT, token.LTE, token.GT, token.GTE:
			return evalCompare(l, r, pred.Op), nil
		case token.AND:
			return and3(l, r), nil
		case token.OR:
			return or3(l, r), nil
		case token.CONCAT:
			return values.Concat(l, r), nil
		case token.PLUS:
			return values.Add(l, r), nil
		case token.MINUS:
			return values.Sub(l, r), nil
		case token.STAR:
			return values.Mul(l, r), nil
		case token.SLASH:
			return values.Div(l, r), nil
		case token.PERCENT:
			return values.Mod(l, r), nil
		case token.IS:
			if truth(l) == 1 {
				return values.NewInteger(0), nil
			}
			return values.NewInteger(1), nil
		default:
			return values.NewNull(), fmt.Errorf("unknown binary operator %s", pred.Op)
		}
	default:
		return values.NewNull(), fmt.Errorf("unknown predicate %T", pred)
	}
}

func evalCompare(a, b values.Value, op token.Kind) values.Value {
	if a.Type == values.Null || b.Type == values.Null {
		return values.NewNull()
	}

	compare := values.Compare(a, b)
	var ok bool
	switch op {
	case token.EQ:
		ok = compare == 0
	case token.NEQ:
		ok = compare != 0
	case token.LT:
		ok = compare < 0
	case token.LTE:
		ok = compare <= 0
	case token.GT:
		ok = compare > 0
	case token.GTE:
		ok = compare >= 0
	default:
		return values.NewNull()
	}
	if ok {
		return values.NewInteger(1)
	}
	return values.NewInteger(0)
}

func truth(v values.Value) int {
	if v.Type == values.Null {
		return -1
	}
	if v.Type == values.Integer && v.Int != 0 {
		return 1
	}
	return 0
}

func and3(l, r values.Value) values.Value {
	if truth(l) == 0 || truth(r) == 0 {
		return values.NewInteger(0)
	} else if truth(l) == 1 && truth(r) == 1 {
		return values.NewInteger(1)
	}
	return values.NewNull()

}

func or3(l, r values.Value) values.Value {
	if truth(l) == 1 || truth(r) == 1 {
		return values.NewInteger(1)
	} else if truth(l) == 0 && truth(r) == 0 {
		return values.NewInteger(0)
	}
	return values.NewNull()
}

func not3(l values.Value) values.Value {
	switch truth(l) {
	case 0:
		return values.NewInteger(1)
	case 1:
		return values.NewInteger(0)
	default:
		return values.NewNull()
	}
}

func EvalConst(expr ast.Expression) (values.Value, error) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return values.Value{}, fmt.Errorf("expected literal, got %T", expr)
	}
	switch lit.Kind {
	case token.INT:
		n, err := strconv.ParseInt(lit.Value, 10, 64)
		if err != nil {
			return values.Value{}, err
		}
		return values.NewInteger(n), nil
	case token.FLOAT:
		f, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return values.Value{}, err
		}
		return values.NewReal(f), nil
	case token.STRING:
		return values.NewText(lit.Value), nil
	case token.NULL:
		return values.NewNull(), nil
	default:
		return values.Value{}, fmt.Errorf("unknown literal kind %s", lit.Kind)
	}
}

func validate(expr ast.Expression, in Scope) error {
	switch e := expr.(type) {
	case *ast.ColumnRef:
		_, err := in.Resolve(e)
		return err
	case *ast.Binary:
		if err := validate(e.Left, in); err != nil {
			return err
		}
		return validate(e.Right, in)
	case *ast.Unary:
		return validate(e.Operand, in)
	}
	return nil
}
