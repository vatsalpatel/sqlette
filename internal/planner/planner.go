package planner

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func ScanTable(cat *catalog.Catalog, tbl *catalog.Table, alias string, where ast.Expression) plan.Node {
	ix := cat.IndexesFor(tbl.Name)
	slices.SortFunc(ix, func(a, b *catalog.Index) int { return strings.Compare(a.Name, b.Name) })
	return Scan(tbl, alias, ix, where)
}

func Scan(tbl *catalog.Table, alias string, indexes []*catalog.Index, where ast.Expression) plan.Node {
	cols := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		cols[i] = c.Name
	}
	seq := func() plan.Node { return filtered(&plan.SeqScan{Table: tbl.Name, Alias: alias, Columns: cols}, where) }
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
		Table:   tbl.Name,
		Alias:   alias,
		Columns: cols,
		Index:   best.Name,
		Column:  best.Columns[0],
		Low:     low,
		High:    high,
	}
	return filtered(scan, residual(parts, used))
}

func Select(cat *catalog.Catalog, stmt *ast.SelectStmt) (plan.Node, error) {
	var node plan.Node
	if stmt.From.Name == "" {
		node = &plan.OneRow{}
		if stmt.Where != nil {
			node = &plan.Filter{Input: node, Predicate: stmt.Where}
		}
	} else {
		tbl, ok := cat.Get(stmt.From.Name)
		if !ok {
			return nil, fmt.Errorf("table %s does not exist", stmt.From.Name)
		}
		node = ScanTable(cat, tbl, stmt.From.Alias, stmt.Where)
	}
	keys, err := sortKeysFor(stmt)
	if err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		node = &plan.Sort{Input: node, Keys: keys}
	}
	node = &plan.Project{Input: node, Columns: stmt.Columns}

	if stmt.Limit != nil {
		count, offset, err := limitFor(stmt)
		if err != nil {
			return nil, err
		}
		node = &plan.Limit{Input: node, Count: count, Offset: offset}
	}
	return node, nil
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

func sortKeysFor(stmt *ast.SelectStmt) ([]plan.SortKey, error) {
	keys := make([]plan.SortKey, 0, len(stmt.OrderBy))
	for _, term := range stmt.OrderBy {
		expr, err := orderExpr(term.Expr, stmt.Columns)
		if err != nil {
			return nil, err
		}
		keys = append(keys, plan.SortKey{Expr: expr, Desc: term.Desc})
	}
	return keys, nil
}

func orderExpr(expr ast.Expression, cols []ast.ResultColumn) (ast.Expression, error) {
	if lit, ok := expr.(*ast.Literal); ok && lit.Kind == token.INT {
		for _, col := range cols {
			if _, ok := col.Expr.(*ast.Star); ok {
				return nil, fmt.Errorf("ORDER BY cardinal is not supported with *")
			}
		}
		n, err := strconv.Atoi(lit.Value)
		if err != nil || n < 1 || n > len(cols) {
			return nil, fmt.Errorf("ORDER BY position %s is out of range", lit.Value)
		}
		return cols[n-1].Expr, nil
	}
	if ref, ok := expr.(*ast.ColumnRef); ok && ref.Table == "" {
		for _, col := range cols {
			if col.Alias != "" && strings.EqualFold(col.Alias, ref.Name) {
				return col.Expr, nil
			}
		}
	}
	return expr, nil
}

func limitFor(stmt *ast.SelectStmt) (count, offset int64, err error) {
	count, err = intLiteral(stmt.Limit)
	if err != nil {
		return 0, 0, err
	}
	if count < 0 {
		count = -1
	}
	if stmt.Offset != nil {
		offset, err = intLiteral(stmt.Offset)
		if err != nil {
			return 0, 0, err
		}
		if offset < 0 {
			offset = 0
		}
	}
	return count, offset, nil
}

func intLiteral(expr ast.Expression) (int64, error) {
	if u, ok := expr.(*ast.Unary); ok && u.Op == token.MINUS {
		n, err := intLiteral(u.Operand)
		return -n, err
	}
	lit, ok := expr.(*ast.Literal)
	if !ok || lit.Kind != token.INT {
		return 0, fmt.Errorf("expected integer literal, got %T", expr)
	}
	return strconv.ParseInt(lit.Value, 10, 64)
}
