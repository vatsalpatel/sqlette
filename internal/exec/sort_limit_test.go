package exec

import (
	"io"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/token"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// Rows are inserted in an order that matches no sort key, so a passing test
// cannot be an accident of rowid order.
var sortRows = []storage.Row{
	{values.NewInteger(5), values.NewText("e")},
	{values.NewInteger(1), values.NewText("a")},
	{values.NewInteger(9), values.NewText("i")},
	{values.NewInteger(3), values.NewText("c")},
}

func sortNode(input plan.Node, keys ...plan.SortKey) *plan.Sort {
	return &plan.Sort{Input: input, Keys: keys}
}

func drain(t *testing.T, op Operator) []storage.Row {
	t.Helper()
	assert.NoError(t, op.Open())
	var out []storage.Row
	for {
		row, err := op.Next()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
		out = append(out, row)
	}
	assert.NoError(t, op.Close())
	return out
}

func TestSortAscendingAndDescending(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	asc := sortNode(seqScanNode(), plan.SortKey{Expr: col("a")})
	assertNames(t, []string{"a", "c", "e", "i"}, runPlan(t, asc, s))

	desc := sortNode(seqScanNode(), plan.SortKey{Expr: col("a"), Desc: true})
	assertNames(t, []string{"i", "e", "c", "a"}, runPlan(t, desc, s))
}

// values.Compare ranks NULL below everything, which is SQLite's ascending
// default, and DESC is a straight reversal rather than a separate rule.
func TestSortNullPlacement(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewInteger(2), values.NewText("two")},
		{values.NewNull(), values.NewText("null")},
		{values.NewInteger(1), values.NewText("one")},
	})

	asc := sortNode(seqScanNode(), plan.SortKey{Expr: col("a")})
	assertNames(t, []string{"null", "one", "two"}, runPlan(t, asc, s))

	desc := sortNode(seqScanNode(), plan.SortKey{Expr: col("a"), Desc: true})
	assertNames(t, []string{"two", "one", "null"}, runPlan(t, desc, s))
}

// Dynamic typing means one column holds every storage class at once, and the
// sort has to follow the same NULL, numeric, text, blob ranking comparisons do.
func TestSortOrdersAcrossStorageClasses(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewText("txt"), values.NewText("text")},
		{values.NewBlob([]byte{1}), values.NewText("blob")},
		{values.NewNull(), values.NewText("null")},
		{values.NewReal(2.5), values.NewText("real")},
		{values.NewInteger(1), values.NewText("int")},
	})

	node := sortNode(seqScanNode(), plan.SortKey{Expr: col("a")})
	assertNames(t, []string{"null", "int", "real", "text", "blob"}, runPlan(t, node, s))
}

// Equal keys keep input order. Without stability the result is reproducible
// only by luck, and a differential run against SQLite turns that into noise.
func TestSortIsStable(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewInteger(1), values.NewText("first")},
		{values.NewInteger(1), values.NewText("second")},
		{values.NewInteger(1), values.NewText("third")},
		{values.NewInteger(0), values.NewText("zero")},
	})

	node := sortNode(seqScanNode(), plan.SortKey{Expr: col("a")})
	assertNames(t, []string{"zero", "first", "second", "third"}, runPlan(t, node, s))
}

func TestSortByMultipleKeysWithMixedDirections(t *testing.T) {
	s := newIndexedStore(t, []int{0}, []storage.Row{
		{values.NewInteger(1), values.NewText("b")},
		{values.NewInteger(2), values.NewText("x")},
		{values.NewInteger(1), values.NewText("a")},
		{values.NewInteger(2), values.NewText("y")},
	})

	node := sortNode(seqScanNode(),
		plan.SortKey{Expr: col("a")},
		plan.SortKey{Expr: col("name"), Desc: true},
	)
	assertNames(t, []string{"b", "a", "y", "x"}, runPlan(t, node, s))
}

func TestSortByAnExpression(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	node := sortNode(seqScanNode(), plan.SortKey{Expr: bin(token.MINUS, intLit("100"), col("a"))})
	assertNames(t, []string{"i", "e", "c", "a"}, runPlan(t, node, s))
}

func TestSortOnEmptyInput(t *testing.T) {
	s := newIndexedStore(t, []int{0}, nil)

	node := sortNode(seqScanNode(), plan.SortKey{Expr: col("a")})
	assert.Equal(t, 0, len(runPlan(t, node, s)))
}

// Stage F opens the inner side of a join once per outer row. A materialising
// operator that forgets to reset returns its rows once and nothing after that.
func TestSortReopens(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	op, _, err := Build(sortNode(seqScanNode(), plan.SortKey{Expr: col("a")}), s)
	assert.NoError(t, err)

	for range 3 {
		assertNames(t, []string{"a", "c", "e", "i"}, drain(t, op))
	}
}

func TestSortRejectsUnknownColumns(t *testing.T) {
	s := newIndexedStore(t, []int{0}, nil)

	_, _, err := Build(sortNode(seqScanNode(), plan.SortKey{Expr: col("nosuch")}), s)
	assert.ErrorContains(t, err, "nosuch")
}

func TestLimitCounts(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	cases := []struct {
		name          string
		count, offset int64
		want          []string
	}{
		{"first two", 2, 0, []string{"e", "a"}},
		{"zero returns nothing", 0, 0, nil},
		{"negative means unlimited", -1, 0, []string{"e", "a", "i", "c"}},
		{"more than there are", 99, 0, []string{"e", "a", "i", "c"}},
		{"offset skips", -1, 2, []string{"i", "c"}},
		{"offset past the end", -1, 99, nil},
		{"offset with count", 1, 1, []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := &plan.Limit{Input: seqScanNode(), Count: c.count, Offset: c.offset}
			got := names(runPlan(t, node, s))
			if len(c.want) == 0 {
				assert.Equal(t, 0, len(got))
				return
			}
			assert.DeepEqual(t, c.want, got)
		})
	}
}

// The whole reason LIMIT sits above the sort: three rows of a sorted result,
// not three arbitrary rows that happen to be sorted afterwards.
func TestLimitTakesTheTopOfTheSortedOrder(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	node := &plan.Limit{
		Input: sortNode(seqScanNode(), plan.SortKey{Expr: col("a"), Desc: true}),
		Count: 2,
	}
	assertNames(t, []string{"i", "e"}, runPlan(t, node, s))
}

func TestLimitReopens(t *testing.T) {
	s := newIndexedStore(t, []int{0}, sortRows)

	op, _, err := Build(&plan.Limit{Input: seqScanNode(), Count: 2, Offset: 1}, s)
	assert.NoError(t, err)

	for range 3 {
		assertNames(t, []string{"a", "i"}, drain(t, op))
	}
}
