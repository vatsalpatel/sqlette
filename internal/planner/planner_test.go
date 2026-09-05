package planner_test

import (
	"fmt"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/planner"
)

func table() *catalog.Table {
	return &catalog.Table{Name: "t", Columns: []catalog.Column{
		{Name: "a", Type: "INT"},
		{Name: "b", Type: "TEXT"},
		{Name: "c", Type: "INT"},
	}}
}

func index(name string, cols ...string) *catalog.Index {
	return &catalog.Index{Name: name, Table: "t", Columns: cols}
}

// where parses a real WHERE clause rather than hand-building AST nodes, so the
// tests read like the SQL they stand for.
func where(t *testing.T, expr string) ast.Expression {
	t.Helper()
	toks, err := lexer.Lex("SELECT * FROM t WHERE " + expr)
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)
	return stmt.(*ast.SelectStmt).Where
}

func mustSeqScan(t *testing.T, node plan.Node) {
	t.Helper()
	mustSeqScanNode(t, node)
}

func mustSeqScanNode(t *testing.T, node plan.Node) *plan.SeqScan {
	t.Helper()
	if f, ok := node.(*plan.Filter); ok {
		node = f.Input
	}
	s, ok := node.(*plan.SeqScan)
	if !ok {
		t.Fatalf("want a SeqScan, got %T", node)
	}
	return s
}

func mustIndexScan(t *testing.T, node plan.Node) *plan.IndexScan {
	t.Helper()
	if f, ok := node.(*plan.Filter); ok {
		node = f.Input
	}
	is, ok := node.(*plan.IndexScan)
	if !ok {
		t.Fatalf("want an IndexScan, got %T", node)
	}
	return is
}

// residualOf returns the predicate left above the scan, or "" if the scan covers
// the whole WHERE clause.
func residualOf(node plan.Node) string {
	if f, ok := node.(*plan.Filter); ok {
		return fmt.Sprintf("%v", f.Predicate)
	}
	return ""
}

func boundOf(b *plan.Bound) string {
	if b == nil {
		return "nil"
	}
	op := "excl"
	if b.Inclusive {
		op = "incl"
	}
	return fmt.Sprintf("%v %s", b.Value, op)
}

func TestScanWithoutWhere(t *testing.T) {
	node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, nil)
	mustSeqScan(t, node)
	assert.Equal(t, "", residualOf(node))
}

// Passing nil indexes must reproduce the pre-index plan exactly. This is the
// hook that lets any query be run both ways and diffed.
func TestScanWithNilIndexesAlwaysSeqScans(t *testing.T) {
	node := planner.Scan(table(), "", nil, where(t, "a = 5"))
	mustSeqScan(t, node)
	assert.Equal(t, "(= a 5)", residualOf(node))
}

func TestScanPicksIndexForEquality(t *testing.T) {
	node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, "a = 5"))

	is := mustIndexScan(t, node)
	assert.Equal(t, "idx_a", is.Index)
	assert.Equal(t, "a", is.Column)
	assert.Equal(t, "5 incl", boundOf(is.Low))
	assert.Equal(t, "5 incl", boundOf(is.High))
	assert.Equal(t, "", residualOf(node)) // fully covered
}

// EXPLAIN renders (= a 5) by checking whether the two bounds hold the *same*
// expression node, so an equality has to reuse one literal at both ends.
func TestEqualityReusesOneExpressionAtBothEnds(t *testing.T) {
	node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, "a = 5"))

	is := mustIndexScan(t, node)
	if is.Low.Value != is.High.Value {
		t.Fatal("an equality should put the same expression node at both bounds")
	}
}

func TestScanRanges(t *testing.T) {
	cases := []struct {
		expr      string
		low, high string
		residual  string
	}{
		{"a > 5", "5 excl", "nil", ""},
		{"a >= 5", "5 incl", "nil", ""},
		{"a < 5", "nil", "5 excl", ""},
		{"a <= 5", "nil", "5 incl", ""},
		{"a > 1 AND a < 9", "1 excl", "9 excl", ""},
		{"a >= 1 AND a <= 9", "1 incl", "9 incl", ""},
		// the mirrored form: the literal is on the left, so the operator flips
		{"5 < a", "5 excl", "nil", ""},
		{"5 > a", "nil", "5 excl", ""},
		{"5 = a", "5 incl", "5 incl", ""},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, c.expr))
			is := mustIndexScan(t, node)
			assert.Equal(t, c.low, boundOf(is.Low))
			assert.Equal(t, c.high, boundOf(is.High))
			assert.Equal(t, c.residual, residualOf(node))
		})
	}
}

// The invariant: a conjunct the scan does not enforce must survive into the
// filter above it. Dropping one silently widens the query.
func TestUncoveredConjunctsStayInTheResidual(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"a = 5 AND b = 'x'", "(= b 'x')"},
		{"a = 5 AND b = 'x' AND c = 1", "(AND (= b 'x') (= c 1))"},
		{"b = 'x' AND a = 5 AND c = 1", "(AND (= b 'x') (= c 1))"},
		// a second predicate on the indexed column: one becomes the bound, the
		// other has to stay behind
		{"a > 1 AND a > 5", "(> a 5)"},
		{"a = 1 AND a = 2", "(= a 2)"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, c.expr))
			mustIndexScan(t, node)
			assert.Equal(t, c.want, residualOf(node))
		})
	}
}

func TestScanFallsBackToSeqScan(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"column is not indexed", "b = 'x'"},
		{"not equal is not a range", "a <> 5"},
		{"or is not a conjunct", "a = 5 OR b = 'x'"},
		{"column compared to a column", "a = c"},
		// a > NULL matches nothing, but NULL sorts below every value, so a bound
		// built from it would seek to the start and return the whole table
		{"null literal bound", "a > NULL"},
		{"null equality", "a = NULL"},
		{"arithmetic on the column", "a + 1 = 5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, c.expr))
			mustSeqScan(t, node)
		})
	}
}

// Only the leading column of a multi-column index can drive a scan.
func TestMultiColumnIndexUsesLeadingColumnOnly(t *testing.T) {
	ix := index("idx_ab", "a", "b")

	node := planner.Scan(table(), "", []*catalog.Index{ix}, where(t, "a = 5 AND b = 'x'"))
	is := mustIndexScan(t, node)
	assert.Equal(t, "a", is.Column)
	assert.Equal(t, "(= b 'x')", residualOf(node))

	// a predicate on the second column alone cannot be served by this index
	mustSeqScan(t, planner.Scan(table(), "", []*catalog.Index{ix}, where(t, "b = 'x'")))
}

// Given a choice, an equality beats a two-sided range beats a one-sided one.
func TestIndexSelectionPrefersTheTighterPredicate(t *testing.T) {
	idxA, idxB := index("idx_a", "a"), index("idx_b", "b")
	indexes := []*catalog.Index{idxA, idxB}

	is := mustIndexScan(t, planner.Scan(table(), "", indexes, where(t, "a > 1 AND b = 'x'")))
	assert.Equal(t, "idx_b", is.Index)

	is = mustIndexScan(t, planner.Scan(table(), "", indexes, where(t, "a = 5 AND b > 'x'")))
	assert.Equal(t, "idx_a", is.Index)

	is = mustIndexScan(t, planner.Scan(table(), "", indexes, where(t, "a > 1 AND a < 9 AND b > 'x'")))
	assert.Equal(t, "idx_a", is.Index)
}

func TestScanIsCaseInsensitiveOnColumnNames(t *testing.T) {
	node := planner.Scan(table(), "", []*catalog.Index{index("idx_a", "a")}, where(t, "A = 5"))
	mustIndexScan(t, node)
}
