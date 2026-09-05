package exec

import (
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/plan"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/token"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// Nothing emits a plan.IndexScan until the planner lands, so these build the
// node by hand. The load-bearing assertion is the differential against
// Filter(SeqScan): the two access paths must select the same rows, compared as
// a set, because index order and rowid order genuinely differ.

// The planner stamps a scan node with the column names it resolved, so the
// executor can build a scope without ever seeing the catalog.
var indexTestColumns = []string{"a", "name"}

// rows deliberately repeat values of a, so the rowid suffix is always in play,
// and are inserted out of order so index order never coincides with rowid order.
var indexTestRows = []storage.Row{
	{values.NewInteger(5), values.NewText("e")},  // rowid 1
	{values.NewInteger(1), values.NewText("a")},  // rowid 2
	{values.NewInteger(9), values.NewText("i")},  // rowid 3
	{values.NewInteger(5), values.NewText("e2")}, // rowid 4
	{values.NewInteger(3), values.NewText("c")},  // rowid 5
	{values.NewInteger(5), values.NewText("e3")}, // rowid 6
	{values.NewInteger(7), values.NewText("g")},  // rowid 7
}

func newIndexedStore(t *testing.T, columns []int, rows []storage.Row) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	assert.NoError(t, err)

	tbl, err := s.CreateTable("t")
	assert.NoError(t, err)
	for _, r := range rows {
		_, err := tbl.Insert(r)
		assert.NoError(t, err)
	}

	ix, err := s.CreateIndex("idx_a", columns, false)
	assert.NoError(t, err)
	assert.NoError(t, tbl.BuildIndex(ix))
	return s
}

func runPlan(t *testing.T, node plan.Node, s *storage.Store) []storage.Row {
	t.Helper()
	op, _, err := Build(node, s)
	assert.NoError(t, err)
	assert.NoError(t, op.Open())
	defer op.Close()

	var out []storage.Row
	for {
		row, err := op.Next()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
		out = append(out, row)
	}
	return out
}

// names pulls the name column out, which identifies each row uniquely and reads
// better in a failure than a slice of values.
func names(rows []storage.Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r[1].Text
	}
	return out
}

func bound(v string, inclusive bool) *plan.Bound {
	return &plan.Bound{Value: intLit(v), Inclusive: inclusive}
}

func indexScanNode(low, high *plan.Bound) *plan.IndexScan {
	return &plan.IndexScan{Table: "t", Columns: indexTestColumns, Index: "idx_a", Column: "a", Low: low, High: high}
}

func assertNames(t *testing.T, want []string, rows []storage.Row) {
	t.Helper()
	got := names(rows)
	if len(got) != len(want) {
		t.Fatalf("got %v (%d rows), want %v (%d rows)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestIndexScanEquality(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	// an equality is one expression at both ends, both inclusive
	lit := intLit("5")
	node := indexScanNode(
		&plan.Bound{Value: lit, Inclusive: true},
		&plan.Bound{Value: lit, Inclusive: true},
	)
	// three rows share a = 5, and they come back in rowid order
	assertNames(t, []string{"e", "e2", "e3"}, runPlan(t, node, s))
}

// The trap of this stage. SeekPrefix lands at or before the first entry holding
// the low value, so an exclusive lower bound has to walk past that entire run.
// It only shows when the boundary value actually exists, and here three rows
// hold it.
func TestIndexScanExclusiveLowSkipsTheBoundaryRun(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	node := indexScanNode(bound("5", false), nil)
	assertNames(t, []string{"g", "i"}, runPlan(t, node, s))
}

func TestIndexScanRanges(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	cases := []struct {
		name      string
		low, high *plan.Bound
		want      []string
	}{
		{"unbounded", nil, nil, []string{"a", "c", "e", "e2", "e3", "g", "i"}},
		{"a >= 5", bound("5", true), nil, []string{"e", "e2", "e3", "g", "i"}},
		{"a > 5", bound("5", false), nil, []string{"g", "i"}},
		{"a <= 5", nil, bound("5", true), []string{"a", "c", "e", "e2", "e3"}},
		{"a < 5", nil, bound("5", false), []string{"a", "c"}},
		{"1 < a < 9", bound("1", false), bound("9", false), []string{"c", "e", "e2", "e3", "g"}},
		{"3 <= a <= 7", bound("3", true), bound("7", true), []string{"c", "e", "e2", "e3", "g"}},
		{"low above everything", bound("99", true), nil, nil},
		{"high below everything", nil, bound("0", true), nil},
		{"empty range", bound("6", true), bound("6", true), nil},
		{"inverted range", bound("9", true), bound("1", true), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertNames(t, c.want, runPlan(t, indexScanNode(c.low, c.high), s))
		})
	}
}

// The real safety net: for each predicate, the index path and the seqscan path
// must select the same rows. Compared as a set, because an index scan returns
// them in key order and a table scan returns them in rowid order.
func TestIndexScanSelectsSameRowsAsSeqScan(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	cases := []struct {
		name      string
		low, high *plan.Bound
		op        token.Kind
		operand   string
	}{
		{"a >= 5", bound("5", true), nil, token.GTE, "5"},
		{"a > 5", bound("5", false), nil, token.GT, "5"},
		{"a <= 5", nil, bound("5", true), token.LTE, "5"},
		{"a < 5", nil, bound("5", false), token.LT, "5"},
		{"a >= 1", bound("1", true), nil, token.GTE, "1"},
		{"a > 9", bound("9", false), nil, token.GT, "9"},
		{"a < 1", nil, bound("1", false), token.LT, "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			viaIndex := names(runPlan(t, indexScanNode(c.low, c.high), s))
			viaScan := names(runPlan(t, &plan.Filter{
				Input:     &plan.SeqScan{Table: "t", Columns: indexTestColumns},
				Predicate: bin(c.op, col("a"), intLit(c.operand)),
			}, s))

			slices.Sort(viaIndex)
			slices.Sort(viaScan)
			if !slices.Equal(viaIndex, viaScan) {
				t.Fatalf("index path returned %v, seqscan path returned %v", viaIndex, viaScan)
			}
		})
	}
}

// DELETE and UPDATE drive their scan through RowScanner, so an index scan has to
// report the rowid of the row it just yielded or mutations built on it hit the
// wrong rows.
func TestIndexScanReportsRowIDs(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	lit := intLit("5")
	op, _, err := Build(indexScanNode(
		&plan.Bound{Value: lit, Inclusive: true},
		&plan.Bound{Value: lit, Inclusive: true},
	), s)
	assert.NoError(t, err)

	scanner, ok := Scanner(op)
	assert.True(t, ok)
	assert.NoError(t, scanner.Open())
	defer scanner.Close()

	var ids []int64
	for {
		if _, err := scanner.Next(); err == io.EOF {
			break
		} else {
			assert.NoError(t, err)
		}
		ids = append(ids, scanner.RowID())
	}
	assert.DeepEqual(t, []int64{1, 4, 6}, ids)
}

// Only the leading column is bounded; the rest are tiebreakers inside the range.
func TestIndexScanBoundsOnlyTheLeadingColumn(t *testing.T) {
	s := newIndexedStore(t, []int{0, 1}, indexTestRows)

	lit := intLit("5")
	node := indexScanNode(
		&plan.Bound{Value: lit, Inclusive: true},
		&plan.Bound{Value: lit, Inclusive: true},
	)
	// within a = 5 the entries now order by name, not by rowid
	assertNames(t, []string{"e", "e2", "e3"}, runPlan(t, node, s))
}

// values.Compare ranks every number below every string, so a > bound picks up
// text rows. That is SQLite's ordering, not a bug to paper over.
func TestIndexScanCrossTypeRange(t *testing.T) {
	rows := []storage.Row{
		{values.NewInteger(1), values.NewText("one")},
		{values.NewText("apple"), values.NewText("text-apple")},
		{values.NewInteger(9), values.NewText("nine")},
		{values.NewNull(), values.NewText("null")},
	}
	s := newIndexedStore(t, []int{0}, rows)

	// NULL sorts below every number, which sorts below every string
	assertNames(t, []string{"null", "one", "nine", "text-apple"},
		runPlan(t, indexScanNode(nil, nil), s))

	assertNames(t, []string{"nine", "text-apple"},
		runPlan(t, indexScanNode(bound("1", false), nil), s))
}

func TestIndexScanOnEmptyTable(t *testing.T) {
	s := newIndexedStore(t, []int{0}, nil)

	assertNames(t, nil, runPlan(t, indexScanNode(nil, nil), s))
	assertNames(t, nil, runPlan(t, indexScanNode(bound("5", true), nil), s))
}

func TestIndexScanBuildErrors(t *testing.T) {
	s := newIndexedStore(t, []int{0}, indexTestRows)

	_, _, err := Build(&plan.IndexScan{Table: "nosuch", Index: "idx_a"}, s)
	if err == nil {
		t.Fatal("Build on an unknown table should error")
	}
	_, _, err = Build(&plan.IndexScan{Table: "t", Index: "nosuch"}, s)
	if err == nil {
		t.Fatal("Build on an unknown index should error")
	}
}
