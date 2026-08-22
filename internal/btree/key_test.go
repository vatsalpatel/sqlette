package btree

import (
	"math"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

// A table key is 8 big-endian bytes, which bytes.Compare would read as an
// *unsigned* integer: -1 encodes as 0xFF..FF and would sort above every positive
// rowid. Nothing in storage produces a negative rowid today, which is exactly
// why the comparator is the only thing standing between that and a tree that is
// silently misordered the day something does.
func TestCompareRowidIsSigned(t *testing.T) {
	cases := []struct {
		a, b int64
		want int
	}{
		{-5, 5, -1},
		{5, -5, 1},
		{-5, -5, 0},
		{-1, 0, -1},
		{0, 1, -1},
		{7, 7, 0},
		{math.MinInt64, math.MaxInt64, -1},
		{math.MaxInt64, math.MinInt64, 1},
	}
	for _, c := range cases {
		if got := compareRowid(rowidKey(c.a), rowidKey(c.b)); got != c.want {
			t.Fatalf("compareRowid(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// The end-to-end version of the above: under bytes.Compare these scan back in
// the order 0, 1, 5, -5, -1 instead of ascending.
func TestTreeNegativeRowids(t *testing.T) {
	tree := newTree(t)
	keys := []int64{5, -5, 0, -1, 1}
	insertKeys(t, tree, keys, 16)
	verifyTree(t, tree, keys, 16)
}

// splitLeaf copies the live cells into a Go slice and then rewrites the page
// underneath them. key(i) is a window into that page, so the copies have to be
// cloned or later cells read bytes that the repack has already overwritten.
//
// Cells are packed in insert order, so inserting ascending rowids rewrites every
// cell at the address it already occupied — identical bytes, bug invisible. The
// insert order here is deliberately not the key order.
func TestSplitPreservesKeysWhenInsertOrderDiffersFromKeyOrder(t *testing.T) {
	const size = 512 // ~7 cells per leaf, so 11 keys force a split

	tree := newTree(t)
	keys := []int64{9, 2, 7, 4, 11, 1, 8, 3, 10, 5, 6}
	insertKeys(t, tree, keys, size)

	verifyTree(t, tree, keys, size)
	checkBalanced(t, tree)
}

func reverseRowid(a, b []byte) int { return -compareRowid(a, b) }

// The comparator belongs to the tree, not to the package. Nodes minted during a
// split — the right sibling from splitLeaf/splitInterior, the new root from
// growRoot — have to inherit it; if initLeaf/initInterior hardcode compareRowid
// instead of taking one, everything passes until the first split and then the
// fresh node sorts the other way. A descending comparator makes that visible:
// the scan must come back descending, splits and all.
//
// (MaxRowID stays rowid-order-specific by design — it is part of the table-tree
// façade, and table trees are always ascending.)
func TestSplitNodesInheritTreeComparator(t *testing.T) {
	const size = 512

	tree := newTree(t)
	tree.cmp = reverseRowid

	keys := []int64{9, 2, 7, 4, 11, 1, 8, 3, 10, 5, 6}
	insertKeys(t, tree, keys, size)

	want := slices.Clone(keys)
	slices.Sort(want)
	slices.Reverse(want)

	c := tree.Cursor()
	defer c.Close()
	var got []int64
	for c.Next() {
		got = append(got, c.RowID())
	}
	assert.NoError(t, c.Err())
	assert.DeepEqual(t, want, got)
}
