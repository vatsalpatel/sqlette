package planner_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/planner"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func planOf(t *testing.T, sql string) plan.Node {
	t.Helper()
	node, err := planner.Select(cat(t), stmt(t, sql))
	assert.NoError(t, err)
	return node
}

func planErr(t *testing.T, sql string) error {
	t.Helper()
	_, err := planner.Select(cat(t), stmt(t, sql))
	return err
}

// The pipeline order is the semantics: LIMIT counts final rows, the projection
// happens under it, and the sort runs before the projection so an ORDER BY can
// name a column the select list never mentions.
func TestSelectPipelineOrder(t *testing.T) {
	node := planOf(t, "SELECT a FROM t WHERE a > 1 ORDER BY b LIMIT 3 OFFSET 1")

	limit, ok := node.(*plan.Limit)
	assert.True(t, ok)
	assert.Equal(t, int64(3), limit.Count)
	assert.Equal(t, int64(1), limit.Offset)

	project, ok := limit.Input.(*plan.Project)
	assert.True(t, ok)

	sort, ok := project.Input.(*plan.Sort)
	assert.True(t, ok)
	assert.Equal(t, 1, len(sort.Keys))

	mustSeqScan(t, sort.Input)
}

func TestNoSortOrLimitNodesWithoutTheClauses(t *testing.T) {
	node := planOf(t, "SELECT a FROM t")

	project, ok := node.(*plan.Project)
	assert.True(t, ok)
	mustSeqScan(t, project.Input)
}

func sortKeys(t *testing.T, sql string) []plan.SortKey {
	t.Helper()
	node := planOf(t, sql)
	if limit, ok := node.(*plan.Limit); ok {
		node = limit.Input
	}
	project, ok := node.(*plan.Project)
	assert.True(t, ok)
	sort, ok := project.Input.(*plan.Sort)
	assert.True(t, ok)
	return sort.Keys
}

func TestOrderByCarriesDirection(t *testing.T) {
	keys := sortKeys(t, "SELECT a FROM t ORDER BY a DESC, b")

	assert.DeepEqual(t, []plan.SortKey{
		{Expr: &ast.ColumnRef{Name: "a"}, Desc: true},
		{Expr: &ast.ColumnRef{Name: "b"}},
	}, keys)
}

// An ordinal names a position in the select list, so it becomes that
// expression and is then evaluated like any other key.
func TestOrderByOrdinal(t *testing.T) {
	keys := sortKeys(t, "SELECT b, a FROM t ORDER BY 2")
	assert.DeepEqual(t, &ast.ColumnRef{Name: "a"}, keys[0].Expr)

	// the last position has to be reachable, which is where an off-by-one hides
	keys = sortKeys(t, "SELECT b, a, c FROM t ORDER BY 3 DESC")
	assert.DeepEqual(t, &ast.ColumnRef{Name: "c"}, keys[0].Expr)
	assert.True(t, keys[0].Desc)

	keys = sortKeys(t, "SELECT a + 1 FROM t ORDER BY 1")
	assert.DeepEqual(t, &ast.Binary{
		Op:    token.PLUS,
		Left:  &ast.ColumnRef{Name: "a"},
		Right: &ast.Literal{Kind: token.INT, Value: "1"},
	}, keys[0].Expr)
}

func TestOrderByOrdinalOutOfRange(t *testing.T) {
	assert.ErrorContains(t, planErr(t, "SELECT a, b FROM t ORDER BY 3"), "range")
	assert.ErrorContains(t, planErr(t, "SELECT a, b FROM t ORDER BY 0"), "range")
}

// Star expansion happens against the scope in exec, so the planner cannot count
// output columns yet and says so instead of guessing.
func TestOrderByOrdinalWithStar(t *testing.T) {
	assert.ErrorContains(t, planErr(t, "SELECT * FROM t ORDER BY 2"), "*")
}

func TestOrderByAlias(t *testing.T) {
	keys := sortKeys(t, "SELECT a * 2 AS d FROM t ORDER BY d")

	assert.DeepEqual(t, &ast.Binary{
		Op:    token.STAR,
		Left:  &ast.ColumnRef{Name: "a"},
		Right: &ast.Literal{Kind: token.INT, Value: "2"},
	}, keys[0].Expr)
}

// An output alias outranks a real column of the same name, which is the one
// place a name means something different in ORDER BY than it does in WHERE.
func TestOrderByAliasShadowsAColumn(t *testing.T) {
	keys := sortKeys(t, "SELECT c AS a FROM t ORDER BY a")
	assert.DeepEqual(t, &ast.ColumnRef{Name: "c"}, keys[0].Expr)
}

func TestOrderByQualifiedNameIsNotAnAlias(t *testing.T) {
	keys := sortKeys(t, "SELECT a * 2 AS d FROM t ORDER BY t.d")
	assert.DeepEqual(t, &ast.ColumnRef{Table: "t", Name: "d"}, keys[0].Expr)
}

func TestLimitValues(t *testing.T) {
	cases := []struct {
		sql           string
		count, offset int64
	}{
		{"SELECT a FROM t LIMIT 5", 5, 0},
		{"SELECT a FROM t LIMIT 5 OFFSET 2", 5, 2},
		{"SELECT a FROM t LIMIT 0", 0, 0},
		{"SELECT a FROM t LIMIT -1", -1, 0},
		{"SELECT a FROM t LIMIT -7", -1, 0},
		{"SELECT a FROM t LIMIT 5 OFFSET -3", 5, 0},
	}
	for _, c := range cases {
		t.Run(c.sql, func(t *testing.T) {
			limit, ok := planOf(t, c.sql).(*plan.Limit)
			assert.True(t, ok)
			assert.Equal(t, c.count, limit.Count)
			assert.Equal(t, c.offset, limit.Offset)
		})
	}
}

func TestLimitRejectsNonLiterals(t *testing.T) {
	for _, sql := range []string{
		"SELECT a FROM t LIMIT a",
		"SELECT a FROM t LIMIT 'x'",
		"SELECT a FROM t LIMIT 1 + 1",
		"SELECT a FROM t LIMIT 5 OFFSET b",
	} {
		t.Run(sql, func(t *testing.T) {
			if planErr(t, sql) == nil {
				t.Fatalf("%q should not plan", sql)
			}
		})
	}
}
