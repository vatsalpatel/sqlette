package planner

import (
	"strings"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func Scan(tbl *catalog.Table, indexes []*catalog.Index, where ast.Expression) plan.Node {
	seq := func() plan.Node { return filtered(&plan.SeqScan{Table: tbl.Name}, where) }
	if where == nil || len(indexes) == 0 {
		return seq()
	}

	parts := conjuncts(where)
	var usable []predicate
	for _, p := range parts {
		if p, ok := match(p); ok {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		return seq()
	}

	var best *catalog.Index
	var bestPreds []predicate
	bestScore := 0
	for _, ix := range indexes {
		if len(ix.Columns) == 0 {
			continue
		}
		var preds []predicate
		for _, p := range usable {
			if strings.EqualFold(p.column, ix.Columns[0]) {
				preds = append(preds, p)
			}
		}
		if s := score(preds); s > bestScore {
			best, bestPreds, bestScore = ix, preds, s
		}
	}
	if best == nil {
		return seq()
	}

	low, high, used := bounds(bestPreds)
	scan := &plan.IndexScan{
		Table:  tbl.Name,
		Index:  best.Name,
		Column: best.Columns[0],
		Low:    low,
		High:   high,
	}
	return filtered(scan, residual(parts, used))
}

type predicate struct {
	src    ast.Expression
	column string
	op     token.Kind
	value  ast.Expression
}

func conjuncts(e ast.Expression) []ast.Expression {
	b, ok := e.(*ast.Binary)
	if !ok || b.Op != token.AND {
		return []ast.Expression{e}
	}
	return append(conjuncts(b.Left), conjuncts(b.Right)...)
}

func match(e ast.Expression) (predicate, bool) {
	b, ok := e.(*ast.Binary)
	if !ok {
		return predicate{}, false
	}
	switch b.Op {
	case token.EQ, token.LT, token.LTE, token.GT, token.GTE:
	default:
		return predicate{}, false
	}

	if c, ok := b.Left.(*ast.ColumnRef); ok {
		if lit, ok := b.Right.(*ast.Literal); ok && usableLiteral(lit) {
			return predicate{src: e, column: c.Name, op: b.Op, value: lit}, ok
		}
	}

	if c, ok := b.Right.(*ast.ColumnRef); ok {
		if lit, ok := b.Left.(*ast.Literal); ok && usableLiteral(lit) {
			return predicate{src: e, column: c.Name, op: mirror(b.Op), value: lit}, ok
		}
	}
	return predicate{}, false
}

func usableLiteral(l *ast.Literal) bool { return l.Kind != token.NULL }

func mirror(op token.Kind) token.Kind {
	switch op {
	case token.LT:
		return token.GT
	case token.GT:
		return token.LT
	case token.LTE:
		return token.GTE
	case token.GTE:
		return token.LTE
	default:
		return op
	}
}

func score(preds []predicate) int {
	var eq, low, high bool
	for _, p := range preds {
		switch p.op {
		case token.EQ:
			eq = true
		case token.LT, token.LTE:
			high = true
		case token.GT, token.GTE:
			low = true
		}
	}
	switch {
	case eq:
		return 3
	case low && high:
		return 2
	case low || high:
		return 1
	default:
		return 0
	}
}

func bounds(preds []predicate) (low, high *plan.Bound, used map[ast.Expression]bool) {
	used = make(map[ast.Expression]bool)
	for _, p := range preds {
		if p.op == token.EQ {
			b := &plan.Bound{Value: p.value, Inclusive: true}
			used[p.src] = true
			return b, b, used
		}
	}

	for _, p := range preds {
		switch p.op {
		case token.GT, token.GTE:
			if low == nil {
				low = &plan.Bound{Value: p.value, Inclusive: p.op == token.GTE}
				used[p.src] = true
			}
		case token.LT, token.LTE:
			if high == nil {
				high = &plan.Bound{Value: p.value, Inclusive: p.op == token.LTE}
				used[p.src] = true
			}
		}
	}
	return low, high, used
}

func residual(parts []ast.Expression, used map[ast.Expression]bool) ast.Expression {
	var out ast.Expression
	for _, p := range parts {
		if used[p] {
			continue
		}
		if out == nil {
			out = p
			continue
		}
		out = &ast.Binary{Left: out, Op: token.AND, Right: p}
	}
	return out
}

func filtered(node plan.Node, where ast.Expression) plan.Node {
	if where == nil {
		return node
	}
	return &plan.Filter{Input: node, Predicate: where}
}
