package exec

import (
	"fmt"
	"io"
	"strconv"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
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

func (s *seqScan) Close() error {
	return s.cursor.Close()
}

type project struct {
	input   Operator
	indices []int
}

func (p *project) Open() error {
	return p.input.Open()
}

func (p *project) Next() (storage.Row, error) {
	row, err := p.input.Next()
	if err != nil {
		return nil, err
	}
	out := make(storage.Row, len(p.indices))
	for i, idx := range p.indices {
		out[i] = row[idx]
	}
	return out, nil
}

func (p *project) Close() error {
	return p.input.Close()
}

type filter struct {
	input  Operator
	pred   ast.Expression
	schema *catalog.Table
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
		v, err := eval(f.pred, row, f.schema)
		if err != nil {
			return nil, err
		}
		if truth(v) == 1 {
			return row, nil
		}
	}
}

func (f *filter) Close() error {
	return f.input.Close()
}

func Build(node plan.Node, store *storage.Store, schema *catalog.Table) (Operator, []string, error) {
	switch n := node.(type) {
	case *plan.SeqScan:
		tbl, ok := store.Table(n.Table)
		if !ok {
			return nil, nil, fmt.Errorf("table %s not found", n.Table)
		}
		names := make([]string, len(schema.Columns))
		for i, c := range schema.Columns {
			names[i] = c.Name
		}
		return &seqScan{table: tbl}, names, nil
	case *plan.Project:
		child, _, err := Build(n.Input, store, schema)
		if err != nil {
			return nil, nil, err
		}
		indices, names, err := bindColumns(n.Columns, schema)
		if err != nil {
			return nil, nil, err
		}
		return &project{input: child, indices: indices}, names, nil
	case *plan.Filter:
		child, _, err := Build(n.Input, store, schema)
		if err != nil {
			return nil, nil, err
		}
		return &filter{input: child, pred: n.Predicate, schema: schema}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown plan node %T", node)
	}
}

func bindColumns(cols []ast.ResultColumn, schema *catalog.Table) ([]int, []string, error) {
	var indices []int
	var names []string
	for _, col := range cols {
		switch expr := col.Expr.(type) {
		case *ast.Star:
			for i, c := range schema.Columns {
				indices = append(indices, i)
				names = append(names, c.Name)
			}
		case *ast.ColumnRef:
			idx, ok := schema.ColumnIndex(expr.Name)
			if !ok {
				return nil, nil, fmt.Errorf("column %s not found", expr.Name)
			}
			indices = append(indices, idx)
			name := expr.Name
			if col.Alias != "" {
				name = col.Alias
			}
			names = append(names, name)
		default:
			return nil, nil, fmt.Errorf("unknown select expression %T", col.Expr)
		}
	}
	return indices, names, nil
}

func eval(pred ast.Expression, row storage.Row, schema *catalog.Table) (values.Value, error) {
	switch pred := pred.(type) {
	case *ast.Literal:
		return EvalConst(pred)
	case *ast.ColumnRef:
		idx, ok := schema.ColumnIndex(pred.Name)
		if !ok {
			return values.NewNull(), fmt.Errorf("column %s not found", pred.Name)
		}
		return row[idx], nil
	case *ast.Unary:
		switch pred.Op {
		case token.NOT:
			op, err := eval(pred.Operand, row, schema)
			if err != nil {
				return values.NewNull(), err
			}
			return not3(op), nil
		default:
			return values.NewNull(), fmt.Errorf("unknown unary operator %s", pred.Op)
		}
	case *ast.Binary:
		l, err := eval(pred.Left, row, schema)
		if err != nil {
			return values.NewNull(), err
		}
		r, err := eval(pred.Right, row, schema)
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
