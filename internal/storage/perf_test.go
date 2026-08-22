package storage

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// The M6 payoff, measured in pages touched rather than wall clock. Timing is a
// headline; page count is a structural claim that holds on any machine and in
// any CI: a table scan touches a page per leaf, so it grows with the table,
// while an index point lookup touches the height of two trees plus a row, which
// does not.
//
// pager.Reads counts every Get, cache hits included. That is deliberate. There
// is no cache eviction until M8, so counting physical reads would measure
// warmup, not algorithmic work.
func TestIndexPointLookupTouchesFarFewerPagesThanAScan(t *testing.T) {
	const n = 100_000

	s, tbl := newTestTable(t)
	ix := addIndex(t, s, tbl, []int{0}, false)

	for i := range n {
		_, err := tbl.Insert(Row{values.NewInteger(int64(i)), values.NewText("row")})
		assert.NoError(t, err)
	}

	target := values.NewInteger(n / 2)

	s.pager.Reads = 0
	scanMatches := 0
	c := tbl.Scan()
	for c.Next() {
		if values.Compare(c.Row()[0], target) == 0 {
			scanMatches++
		}
	}
	assert.NoError(t, c.Err())
	assert.NoError(t, c.Close())
	scanPages := s.pager.Reads

	s.pager.Reads = 0
	indexMatches := 0
	ic := tbl.IndexScan(ix, &Bound{Value: target, Inclusive: true}, &Bound{Value: target, Inclusive: true})
	for ic.Next() {
		indexMatches++
	}
	assert.NoError(t, ic.Err())
	assert.NoError(t, ic.Close())
	indexPages := s.pager.Reads

	// both paths must answer the same question
	assert.Equal(t, 1, scanMatches)
	assert.Equal(t, scanMatches, indexMatches)

	t.Logf("point lookup over %d rows: %d pages via the index, %d pages via a scan (%.0fx fewer)",
		n, indexPages, scanPages, float64(scanPages)/float64(indexPages))

	// The index path is bounded by tree height, so a generous constant holds
	// however the fanout works out.
	if indexPages > 20 {
		t.Errorf("index lookup touched %d pages, want a small constant bounded by tree height", indexPages)
	}
	// The scan path is bounded by table size, and must be visibly larger.
	if scanPages < 200 {
		t.Errorf("scan touched only %d pages over %d rows, so this is not measuring what it claims", scanPages, n)
	}
	if scanPages/indexPages < 20 {
		t.Errorf("index touched %d pages vs %d for the scan, want at least 20x fewer", indexPages, scanPages)
	}
}

// A range scan should also stay proportional to what it returns rather than to
// the table: ten rows out of a hundred thousand must not cost a full walk.
func TestIndexRangeScanCostTracksResultSize(t *testing.T) {
	const n = 100_000

	s, tbl := newTestTable(t)
	ix := addIndex(t, s, tbl, []int{0}, false)

	for i := range n {
		_, err := tbl.Insert(Row{values.NewInteger(int64(i)), values.NewText("row")})
		assert.NoError(t, err)
	}

	s.pager.Reads = 0
	got := 0
	ic := tbl.IndexScan(ix,
		&Bound{Value: values.NewInteger(50_000), Inclusive: true},
		&Bound{Value: values.NewInteger(50_009), Inclusive: true})
	for ic.Next() {
		got++
	}
	assert.NoError(t, ic.Err())
	assert.NoError(t, ic.Close())

	assert.Equal(t, 10, got)
	t.Logf("10-row range over %d rows touched %d pages", n, s.pager.Reads)
	if s.pager.Reads > 60 {
		t.Errorf("10-row range touched %d pages, want a cost tied to the result size", s.pager.Reads)
	}
}
