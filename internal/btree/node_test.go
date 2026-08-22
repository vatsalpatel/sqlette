package btree

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

func payloadFor(rowid int64) []byte {
	return fmt.Appendf(nil, "v%d", rowid)
}

// rowidKey encodes a rowid the way a table cell stores it, so the node-level
// tests can keep talking in plain integers.
func rowidKey(rowid int64) []byte {
	k := make([]byte, rowidKeySize)
	putRowid(k, rowid)
	return k
}

func freshLeaf() node {
	return initLeaf(&pager.Page{}, tableTree, compareRowid)
}

func freshInterior(leftmost pager.PageID) node {
	return initInterior(&pager.Page{}, leftmost, tableTree, compareRowid)
}

func TestLeafEmpty(t *testing.T) {
	n := freshLeaf()
	assert.True(t, n.isLeaf())
	assert.Equal(t, 0, n.numCells())
	_, found := n.search(rowidKey(42))
	assert.False(t, found)
}

// Cells are stored in key order regardless of insert order — the whole point of
// the pointer array. Distinct payloads catch a key/payload mix-up.
func TestLeafStoresCellsSorted(t *testing.T) {
	n := freshLeaf()
	for _, rowid := range []int64{30, 10, 20} {
		assert.True(t, n.insertLeaf(rowidKey(rowid), payloadFor(rowid)))
	}

	assert.Equal(t, 3, n.numCells())
	for i, want := range []int64{10, 20, 30} {
		assert.Equal(t, want, rowidOf(n.key(i)))
		assert.True(t, bytes.Equal(payloadFor(want), n.payload(i)))
	}
}

// search returns the lower-bound slot and whether it was an exact hit — the slot
// is where an absent key would be inserted.
func TestLeafSearch(t *testing.T) {
	n := freshLeaf()
	for _, rowid := range []int64{10, 20, 30} {
		n.insertLeaf(rowidKey(rowid), payloadFor(rowid))
	}

	i, found := n.search(rowidKey(20))
	assert.True(t, found)
	assert.Equal(t, 1, i)

	i, found = n.search(rowidKey(25))
	assert.False(t, found)
	assert.Equal(t, 2, i)

	i, found = n.search(rowidKey(5))
	assert.False(t, found)
	assert.Equal(t, 0, i)

	i, found = n.search(rowidKey(35))
	assert.False(t, found)
	assert.Equal(t, 3, i)
}

func TestLeafPayloadSizes(t *testing.T) {
	n := freshLeaf()
	assert.True(t, n.insertLeaf(rowidKey(1), []byte{}))
	assert.True(t, n.insertLeaf(rowidKey(2), []byte("hello")))
	big := bytes.Repeat([]byte{0xab}, 200)
	assert.True(t, n.insertLeaf(rowidKey(3), big))

	assert.Equal(t, 0, len(n.payload(0)))
	assert.True(t, bytes.Equal([]byte("hello"), n.payload(1)))
	assert.True(t, bytes.Equal(big, n.payload(2)))
}

// "Full" is spatial, not a count: insertLeaf returns false once the cell no
// longer fits the free space. Everything that fit stays intact and ordered.
func TestLeafFillUntilFull(t *testing.T) {
	n := freshLeaf()
	payload := bytes.Repeat([]byte{0x7f}, 200)

	count := 0
	for rowid := int64(1); rowid <= 1000; rowid++ {
		if !n.insertLeaf(rowidKey(rowid), payload) {
			break
		}
		count++
	}

	assert.True(t, count > 0)
	if count >= 1000 {
		t.Fatal("page never reported full — insertLeaf should return false when out of room")
	}
	assert.Equal(t, count, n.numCells())
	for i := 0; i < count; i++ {
		assert.Equal(t, int64(i+1), rowidOf(n.key(i)))
	}
}

func TestLeafRightSibling(t *testing.T) {
	n := freshLeaf()
	assert.Equal(t, pager.PageID(0), n.rightSibling())
	n.setRightSibling(7)
	assert.Equal(t, pager.PageID(7), n.rightSibling())
}

// deleteLeaf repacks by copying the survivors out and rebuilding the page. Now
// that key(i) returns a window into that page, the survivors have to be cloned
// before the rebuild starts overwriting them — and cells are packed in insert
// order, so an ascending insert rewrites every cell at the address it already
// occupied and hides the bug. This insert order is deliberately not key order.
func TestLeafDeleteRepackPreservesKeys(t *testing.T) {
	n := freshLeaf()
	for _, rowid := range []int64{40, 10, 50, 20, 30} {
		assert.True(t, n.insertLeaf(rowidKey(rowid), payloadFor(rowid)))
	}

	slot, found := n.search(rowidKey(30))
	assert.True(t, found)
	n.deleteLeaf(slot)

	assert.Equal(t, 4, n.numCells())
	for i, want := range []int64{10, 20, 40, 50} {
		assert.Equal(t, want, rowidOf(n.key(i)))
		assert.True(t, bytes.Equal(payloadFor(want), n.payload(i)))
	}
}

func TestInteriorStoresChildren(t *testing.T) {
	n := freshInterior(100)
	assert.False(t, n.isLeaf())
	assert.Equal(t, pager.PageID(100), n.leftmostChild())

	entries := []struct {
		sep   int64
		child pager.PageID
	}{{30, 5}, {10, 3}, {20, 4}}
	for _, e := range entries {
		assert.True(t, n.insertInterior(rowidKey(e.sep), e.child))
	}

	assert.Equal(t, 3, n.numCells())
	wantKeys := []int64{10, 20, 30}
	wantChildren := []pager.PageID{3, 4, 5}
	for i := range wantKeys {
		assert.Equal(t, wantKeys[i], rowidOf(n.key(i)))
		assert.Equal(t, wantChildren[i], n.child(i))
	}
}

// Routing is left-inclusive, equal-key-routes-right: a key >= a separator
// descends into that separator's child. childPage hides the leftmost/cell split.
func TestInteriorRouting(t *testing.T) {
	n := freshInterior(100)
	assert.True(t, n.insertInterior(rowidKey(10), 3))
	assert.True(t, n.insertInterior(rowidKey(20), 4))
	assert.True(t, n.insertInterior(rowidKey(30), 5))

	cases := []struct {
		key  int64
		want pager.PageID
	}{
		{5, 100}, {9, 100},
		{10, 3}, {15, 3}, {19, 3},
		{20, 4}, {25, 4},
		{30, 5}, {99, 5},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, n.childPage(rowidKey(c.key)))
	}
}
