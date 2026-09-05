package exec

import (
	"io"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/token"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func seqScanNode() *plan.SeqScan {
	return &plan.SeqScan{Table: "t", Columns: indexTestColumns}
}

func resultCol(e ast.Expression, alias string) ast.ResultColumn {
	return ast.ResultColumn{Expr: e, Alias: alias}
}

// The projection stopped copying slots and started evaluating, so a select list
// item is now an arbitrary expression over the input scope.
func TestProjectEvaluatesExpressions(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewInteger(2), values.NewText("ada")},
	})

	node := &plan.Project{Input: seqScanNode(), Columns: []ast.ResultColumn{
		resultCol(bin(token.PLUS, col("a"), intLit("1")), ""),
		resultCol(bin(token.CONCAT, col("name"), &ast.Literal{Kind: token.STRING, Value: "!"}), ""),
		resultCol(intLit("7"), ""),
	}}

	rows := runPlan(t, node, s)
	assert.Equal(t, 1, len(rows))
	assert.DeepEqual(t, storage.Row{
		values.NewInteger(3),
		values.NewText("ada!"),
		values.NewInteger(7),
	}, rows[0])
}

// Output names: the alias wins, a bare column keeps its own name, and anything
// else is named after the expression, since the AST does not keep source text.
func TestProjectOutputScope(t *testing.T) {
	s := newIndexedStore(t, []int{0}, nil)

	node := &plan.Project{Input: seqScanNode(), Columns: []ast.ResultColumn{
		resultCol(col("a"), ""),
		resultCol(col("a"), "renamed"),
		resultCol(bin(token.PLUS, col("a"), intLit("1")), ""),
		resultCol(bin(token.PLUS, col("a"), intLit("1")), "sum"),
	}}

	_, scope, err := Build(node, s)
	assert.NoError(t, err)
	assert.DeepEqual(t, Scope{
		{Table: "t", Name: "a"},
		{Name: "renamed"},
		{Name: "(+ a 1)"},
		{Name: "sum"},
	}, scope)
}

func TestProjectStarExpandsFromTheScope(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewInteger(2), values.NewText("ada")},
	})

	node := &plan.Project{Input: seqScanNode(), Columns: []ast.ResultColumn{
		resultCol(&ast.Star{}, ""),
	}}

	_, scope, err := Build(node, s)
	assert.NoError(t, err)
	assert.DeepEqual(t, Scope{{Table: "t", Name: "a"}, {Table: "t", Name: "name"}}, scope)
	assert.DeepEqual(t, storage.Row{values.NewInteger(2), values.NewText("ada")}, runPlan(t, node, s)[0])
}

// An unknown column has to fail when the plan is built, not when the first row
// arrives. On an empty table no row ever arrives, so a run-time-only check makes
// the query quietly succeed with no rows at all.
func TestBuildRejectsUnknownColumns(t *testing.T) {
	s := newIndexedStore(t, []int{0}, nil)

	cases := map[string]plan.Node{
		"in a filter": &plan.Filter{
			Input:     seqScanNode(),
			Predicate: bin(token.EQ, col("nosuch"), intLit("1")),
		},
		"in a projection": &plan.Project{
			Input:   seqScanNode(),
			Columns: []ast.ResultColumn{resultCol(bin(token.PLUS, col("nosuch"), intLit("1")), "")},
		},
		"under a unary": &plan.Filter{
			Input:     seqScanNode(),
			Predicate: &ast.Unary{Op: token.NOT, Operand: col("nosuch")},
		},
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := Build(node, s)
			assert.ErrorContains(t, err, "nosuch")
		})
	}
}

func TestOneRowYieldsExactlyOneEmptyRow(t *testing.T) {
	op, scope, err := Build(&plan.OneRow{}, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(scope))
	assert.NoError(t, op.Open())

	row, err := op.Next()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(row))

	_, err = op.Next()
	assert.Equal(t, io.EOF, err)
	assert.NoError(t, op.Close())
}

// Stage F opens the inner side of a join once per outer row, so an operator that
// only works the first time is a bug waiting for that stage. oneRow is the
// smallest place to pin the rule.
func TestOneRowReopens(t *testing.T) {
	op, _, err := Build(&plan.OneRow{}, nil)
	assert.NoError(t, err)

	for range 3 {
		assert.NoError(t, op.Open())
		_, err := op.Next()
		assert.NoError(t, err)
		_, err = op.Next()
		assert.Equal(t, io.EOF, err)
		assert.NoError(t, op.Close())
	}
}
