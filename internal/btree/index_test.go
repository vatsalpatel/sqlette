package btree

import (
	"bytes"
	"fmt"
	"math/rand"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

// Stage B's tests drive index trees through a plain bytes.Compare, deliberately
// never touching record or values. If these pass, the comparator injection is
// doing real work: the tree is ordering keys it cannot interpret. Stage C is
// where the ordering becomes SQL value semantics.

// indexKeyFor builds a key that sorts by n under bytes.Compare — fixed-width
// zero padding makes lexicographic order agree with numeric order. size pads the
// key out to force splits: an index cell carries no payload, so key length is
// the only lever on how many entries fit per page.
func indexKeyFor(n int, size int) []byte {
	k := fmt.Appendf(nil, "key-%06d", n)
	for len(k) < size {
		k = append(k, 'x')
	}
	return k
}

// keyOfSize makes a key of an exact byte length, for probing the length-prefix
// encoding itself rather than the tree.
func keyOfSize(ord byte, size int) []byte {
	k := bytes.Repeat([]byte{'a'}, size)
	k[0] = ord
	return k
}

func seqInts(lo, hi int) []int {
	out := make([]int, 0, hi-lo)
	for n := lo; n < hi; n++ {
		out = append(out, n)
	}
	return out
}

func newIndexTree(t *testing.T) *Tree {
	t.Helper()
	p, err := pager.Open(filepath.Join(t.TempDir(), "index.db"))
	assert.NoError(t, err)
	tree, err := CreateIndex(p, bytes.Compare)
	assert.NoError(t, err)
	return tree
}

func insertIndexKeys(t *testing.T, tree *Tree, ns []int, size int) {
	t.Helper()
	for _, n := range ns {
		if err := tree.InsertKey(indexKeyFor(n, size)); err != nil {
			t.Fatalf("InsertIndex(%d): %v", n, err)
		}
	}
}

// scanKeys drains a cursor. The clone is not politeness: Key() hands back a
// window into the page, and the next call to Next() can move to another leaf.
func scanKeys(t *testing.T, c *Cursor) [][]byte {
	t.Helper()
	defer c.Close()
	var got [][]byte
	for c.Next() {
		got = append(got, bytes.Clone(c.Key()))
	}
	assert.NoError(t, c.Err())
	return got
}

func verifyIndex(t *testing.T, tree *Tree, ns []int, size int) {
	t.Helper()
	want := slices.Clone(ns)
	slices.Sort(want)

	got := scanKeys(t, tree.Cursor())
	if len(got) != len(want) {
		t.Fatalf("scan returned %d keys, want %d", len(got), len(want))
	}
	for i, n := range want {
		if !bytes.Equal(indexKeyFor(n, size), got[i]) {
			t.Fatalf("scan[%d] = %q, want %q", i, got[i], indexKeyFor(n, size))
		}
	}
}

func assertSeekYields(t *testing.T, tree *Tree, seek, size int, want []int) {
	t.Helper()
	got := scanKeys(t, tree.Seek(indexKeyFor(seek, size)))
	if len(got) != len(want) {
		t.Fatalf("Seek(%d) yielded %d keys, want %d", seek, len(got), len(want))
	}
	for i, n := range want {
		if !bytes.Equal(indexKeyFor(n, size), got[i]) {
			t.Fatalf("Seek(%d)[%d] = %q, want %q", seek, i, got[i], indexKeyFor(n, size))
		}
	}
}

func TestIndexTreeEmpty(t *testing.T) {
	tree := newIndexTree(t)

	assert.Equal(t, 0, len(scanKeys(t, tree.Cursor())))
	assert.Equal(t, 0, len(scanKeys(t, tree.Seek(indexKeyFor(1, 0)))))

	deleted, err := tree.DeleteKey(indexKeyFor(1, 0))
	assert.NoError(t, err)
	assert.False(t, deleted)
}

func TestIndexTreeInsertScanSorted(t *testing.T) {
	tree := newIndexTree(t)
	ns := []int{30, 10, 50, 20, 40}
	insertIndexKeys(t, tree, ns, 0)
	verifyIndex(t, tree, ns, 0)
}

// A table key is always 8 bytes; an index key carries its own length, so lengths
// can differ wildly within one page. 127/128 is the interesting boundary — that
// is where the uvarint length prefix widens from one byte to two, and getting
// the cell arithmetic wrong there shifts every following field by a byte.
//
// These all fit in a single leaf on purpose: this test is about the encoding,
// not about splits.
func TestIndexTreeVariableLengthKeys(t *testing.T) {
	tree := newIndexTree(t)

	sizes := []int{1, 2, 7, 63, 126, 127, 128, 129, 300, 900}
	var keys [][]byte
	for i, size := range sizes {
		keys = append(keys, keyOfSize(byte('a'+i), size))
	}
	for _, k := range keys {
		assert.NoError(t, tree.InsertKey(k))
	}

	want := slices.Clone(keys)
	slices.SortFunc(want, bytes.Compare)

	got := scanKeys(t, tree.Cursor())
	if len(got) != len(want) {
		t.Fatalf("scan returned %d keys, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(want[i], got[i]) {
			t.Fatalf("scan[%d]: len %d, want len %d", i, len(got[i]), len(want[i]))
		}
	}
}

// An index interior cell holds a full separator key, not the 12 bytes a table
// interior gets away with, so index trees run out of fan-out far sooner and grow
// tall on fewer entries. 400 fat keys is enough to split interior pages, which
// is the path that only runs once the root has already split.
func TestIndexTreeSplitsAndGrowsRoot(t *testing.T) {
	const size = 500

	tree := newIndexTree(t)
	r := rand.New(rand.NewSource(2))
	ns := r.Perm(400)
	insertIndexKeys(t, tree, ns, size)

	verifyIndex(t, tree, ns, size)
	checkBalanced(t, tree)
	if h := treeHeight(t, tree); h < 2 {
		t.Fatalf("height %d, want >= 2 so interior pages split as well as leaves", h)
	}
}

// Seek positions the cursor *before* the first entry >= key, so the first Next()
// yields it — the same convention Cursor() uses with slot -1. Asserting the
// whole remaining sequence rather than just the first key catches a cursor that
// starts in the right place but then traverses wrong.
func TestIndexTreeSeek(t *testing.T) {
	tree := newIndexTree(t)
	ns := []int{10, 20, 30, 40, 50}
	insertIndexKeys(t, tree, ns, 0)

	cases := []struct {
		name string
		seek int
		want []int
	}{
		{"before the first key", 5, []int{10, 20, 30, 40, 50}},
		{"exact hit on the first key", 10, []int{10, 20, 30, 40, 50}},
		{"exact hit in the middle", 30, []int{30, 40, 50}},
		{"miss lands on the next greater", 25, []int{30, 40, 50}},
		{"exact hit on the last key", 50, []int{50}},
		{"past the last key", 99, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertSeekYields(t, tree, c.seek, 0, c.want)
		})
	}
}

// The single-leaf cases above never exercise childPage. This one descends
// through interior pages to land mid-tree, then runs the leaf chain to the end.
func TestIndexTreeSeekRoutesThroughInteriors(t *testing.T) {
	const size = 500

	tree := newIndexTree(t)
	insertIndexKeys(t, tree, seqInts(0, 400), size)
	if h := treeHeight(t, tree); h < 1 {
		t.Fatalf("height %d, want a tree with interior pages to route through", h)
	}

	assertSeekYields(t, tree, 250, size, seqInts(250, 400))
}

// M5 tolerates underfull and entirely empty leaves — no merge, no rebalance. A
// Seek landing in a hole has to walk forward to the next non-empty leaf instead
// of reporting the end of the tree. The deleted run is far wider than one leaf,
// so at least one leaf is certainly empty whatever the split boundaries were.
func TestIndexTreeSeekIntoEmptyLeaf(t *testing.T) {
	const size = 500

	tree := newIndexTree(t)
	insertIndexKeys(t, tree, seqInts(0, 200), size)

	for n := 50; n < 150; n++ {
		deleted, err := tree.DeleteKey(indexKeyFor(n, size))
		assert.NoError(t, err)
		assert.True(t, deleted)
	}

	assertSeekYields(t, tree, 100, size, seqInts(150, 200))

	survivors := append(seqInts(0, 50), seqInts(150, 200)...)
	verifyIndex(t, tree, survivors, size)
}

func TestIndexTreeDeleteByKey(t *testing.T) {
	const size = 300

	tree := newIndexTree(t)
	ns := seqInts(0, 200)
	insertIndexKeys(t, tree, ns, size)

	var survivors []int
	for _, n := range ns {
		if n%3 != 0 {
			survivors = append(survivors, n)
			continue
		}
		deleted, err := tree.DeleteKey(indexKeyFor(n, size))
		assert.NoError(t, err)
		assert.True(t, deleted)
	}

	verifyIndex(t, tree, survivors, size)

	deleted, err := tree.DeleteKey(indexKeyFor(9999, size))
	assert.NoError(t, err)
	assert.False(t, deleted)
}

func TestIndexTreeReopen(t *testing.T) {
	const size = 500

	path := filepath.Join(t.TempDir(), "index.db")
	p, err := pager.Open(path)
	assert.NoError(t, err)
	tree, err := CreateIndex(p, bytes.Compare)
	assert.NoError(t, err)

	ns := seqInts(0, 300)
	insertIndexKeys(t, tree, ns, size)
	root := tree.Root()
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	tree2 := OpenIndex(p2, root, bytes.Compare)
	verifyIndex(t, tree2, ns, size)
	checkBalanced(t, tree2)
}

// Both flavours live in one file, and a page's own type byte is the only thing
// that says how to decode its cells — neither tree gets to tell the other's
// pages what they are. Interleaving the inserts shuffles the two trees' pages
// together on disk, so a decoder that guessed from context would guess wrong.
func TestTableAndIndexTreesShareOneFile(t *testing.T) {
	const size = 500

	path := filepath.Join(t.TempDir(), "both.db")
	p, err := pager.Open(path)
	assert.NoError(t, err)

	table, err := Create(p)
	assert.NoError(t, err)
	index, err := CreateIndex(p, bytes.Compare)
	assert.NoError(t, err)

	for n := 0; n < 200; n++ {
		assert.NoError(t, table.Insert(int64(n), fatPayload(int64(n), size)))
		assert.NoError(t, index.InsertKey(indexKeyFor(n, size)))
	}

	tableRoot, indexRoot := table.Root(), index.Root()
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	verifyTree(t, Open(p2, tableRoot), seqKeys(0, 200), size)
	verifyIndex(t, OpenIndex(p2, indexRoot, bytes.Compare), seqInts(0, 200), size)
}

func TestIndexTreeRejectsOversizedKey(t *testing.T) {
	tree := newIndexTree(t)

	if err := tree.InsertKey(make([]byte, pager.PageSize)); err == nil {
		t.Fatal("InsertIndex of a key larger than a page should error, got nil")
	}

	// the tree is still usable afterwards
	assert.NoError(t, tree.InsertKey(indexKeyFor(1, 0)))
	verifyIndex(t, tree, []int{1}, 0)
}

func TestIndexTreeRandomStress(t *testing.T) {
	const (
		n    = 1000
		size = 200
	)

	tree := newIndexTree(t)
	r := rand.New(rand.NewSource(3))
	ns := r.Perm(n)
	insertIndexKeys(t, tree, ns, size)

	var survivors []int
	for _, k := range ns {
		if k%7 != 0 {
			survivors = append(survivors, k)
			continue
		}
		deleted, err := tree.DeleteKey(indexKeyFor(k, size))
		assert.NoError(t, err)
		assert.True(t, deleted)
	}

	verifyIndex(t, tree, survivors, size)
	checkBalanced(t, tree)

	slices.Sort(survivors)
	mid := survivors[len(survivors)/2]
	assertSeekYields(t, tree, mid, size, survivors[len(survivors)/2:])
}
