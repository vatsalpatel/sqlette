package planner_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/planner"
)

// planner.Select is the only place a SELECT plan is built, which is what keeps
// EXPLAIN describing the plan that actually runs.
func cat(t *testing.T, indexes ...*catalog.Index) *catalog.Catalog {
	t.Helper()
	c := catalog.New()
	assert.NoError(t, c.Create(table()))
	for _, ix := range indexes {
		assert.NoError(t, c.CreateIndex(ix))
	}
	return c
}

func stmt(t *testing.T, sql string) *ast.SelectStmt {
	t.Helper()
	toks, err := lexer.Lex(sql)
	assert.NoError(t, err)
	parsed, err := parser.Parse(toks)
	assert.NoError(t, err)
	return parsed.(*ast.SelectStmt)
}

// under returns what the projection reads from, since every SELECT plan is a
// Project on top of something.
func under(t *testing.T, node plan.Node) plan.Node {
	t.Helper()
	p, ok := node.(*plan.Project)
	if !ok {
		t.Fatalf("want a Project at the top, got %T", node)
	}
	return p.Input
}

func TestSelectPlansAProjectOverAScan(t *testing.T) {
	s := stmt(t, "SELECT a, b FROM t")
	node, err := planner.Select(cat(t), s)
	assert.NoError(t, err)

	p := node.(*plan.Project)
	assert.DeepEqual(t, s.Columns, p.Columns)

	scan, ok := p.Input.(*plan.SeqScan)
	assert.True(t, ok)
	assert.Equal(t, "t", scan.Table)
	assert.Equal(t, "", scan.Alias)
	assert.DeepEqual(t, []string{"a", "b", "c"}, scan.Columns)
}

func TestSelectStampsTheAlias(t *testing.T) {
	node, err := planner.Select(cat(t), stmt(t, "SELECT * FROM t AS x WHERE a = 5"))
	assert.NoError(t, err)

	scan := mustSeqScanNode(t, under(t, node))
	assert.Equal(t, "x", scan.Alias)
}

func TestSelectWithoutFrom(t *testing.T) {
	node, err := planner.Select(cat(t), stmt(t, "SELECT 1"))
	assert.NoError(t, err)

	_, ok := under(t, node).(*plan.OneRow)
	assert.True(t, ok)
}

func TestSelectWithoutFromKeepsTheWhere(t *testing.T) {
	node, err := planner.Select(cat(t), stmt(t, "SELECT 1 WHERE 1 = 0"))
	assert.NoError(t, err)

	f, ok := under(t, node).(*plan.Filter)
	assert.True(t, ok)
	assert.Equal(t, "(= 1 0)", residualOf(f))

	_, ok = f.Input.(*plan.OneRow)
	assert.True(t, ok)
}

func TestSelectUnknownTable(t *testing.T) {
	_, err := planner.Select(cat(t), stmt(t, "SELECT * FROM nosuch"))
	assert.ErrorContains(t, err, "does not exist")
}

// Access-path selection has to survive the move out of the engine: the scan
// under the projection is still chosen by Scan, indexes and all.
func TestSelectStillChoosesAnIndex(t *testing.T) {
	c := cat(t, index("idx_a", "a"))

	node, err := planner.Select(c, stmt(t, "SELECT * FROM t WHERE a = 5 AND b = 'x'"))
	assert.NoError(t, err)

	inner := under(t, node)
	is := mustIndexScan(t, inner)
	assert.Equal(t, "idx_a", is.Index)
	assert.Equal(t, "a", is.Column)
	assert.DeepEqual(t, []string{"a", "b", "c"}, is.Columns)
	assert.Equal(t, "(= b 'x')", residualOf(inner))
}

// Catalog.IndexesFor walks a map, so without the sort in ScanTable a tie between
// two equally good indexes would resolve differently run to run. The loop is
// there because one pass through a randomised map proves nothing.
func TestIndexChoiceIsDeterministicUnderATie(t *testing.T) {
	c := cat(t, index("idx_a_dup", "a"), index("idx_a", "a"))

	for range 20 {
		node, err := planner.Select(c, stmt(t, "SELECT * FROM t WHERE a = 5"))
		assert.NoError(t, err)
		assert.Equal(t, "idx_a", mustIndexScan(t, under(t, node)).Index)
	}
}
