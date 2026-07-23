package btree

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

// fatPayload makes a size-byte payload whose first bytes encode the rowid, so a
// key/payload mix-up is caught. size controls how many rows fit per leaf: big
// payloads force splits after a handful of inserts.
func fatPayload(rowid int64, size int) []byte {
	p := make([]byte, size)
	copy(p, payloadFor(rowid))
	return p
}

func seqKeys(lo, hi int64) []int64 {
	out := make([]int64, 0, hi-lo)
	for k := lo; k < hi; k++ {
		out = append(out, k)
	}
	return out
}

func newTree(t *testing.T) *Tree {
	t.Helper()
	p, err := pager.Open(filepath.Join(t.TempDir(), "test.db"))
	assert.NoError(t, err)
	tree, err := Create(p)
	assert.NoError(t, err)
	return tree
}

func insertKeys(t *testing.T, tree *Tree, keys []int64, size int) {
	t.Helper()
	for _, k := range keys {
		if err := tree.Insert(k, fatPayload(k, size)); err != nil {
			t.Fatalf("Insert(%d): %v", k, err)
		}
	}
}

// verifyTree is the workhorse: every key reads back its payload via Get, and a
// full scan yields every (rowid, payload) in ascending rowid order. keys is the
// full set currently in the tree, in any order.
func verifyTree(t *testing.T, tree *Tree, keys []int64, size int) {
	t.Helper()
	for _, k := range keys {
		got, found, err := tree.Get(k)
		if err != nil {
			t.Fatalf("Get(%d): %v", k, err)
		}
		if !found {
			t.Fatalf("Get(%d): not found", k)
		}
		if !bytes.Equal(got, fatPayload(k, size)) {
			t.Fatalf("Get(%d): payload mismatch", k)
		}
	}

	want := slices.Clone(keys)
	slices.Sort(want)

	c := tree.Cursor()
	defer c.Close()
	i := 0
	for c.Next() {
		if i >= len(want) {
			t.Fatalf("scan returned more than %d rows", len(want))
		}
		if c.RowID() != want[i] {
			t.Fatalf("scan[%d]: rowid = %d, want %d", i, c.RowID(), want[i])
		}
		if !bytes.Equal(c.Payload(), fatPayload(want[i], size)) {
			t.Fatalf("scan[%d]: payload mismatch for rowid %d", i, want[i])
		}
		i++
	}
	if err := c.Err(); err != nil {
		t.Fatalf("cursor error: %v", err)
	}
	if i != len(want) {
		t.Fatalf("scan returned %d rows, want %d", i, len(want))
	}
}

// treeHeight descends leftmost children to the deepest leaf; 0 = the root is a leaf.
func treeHeight(t *testing.T, tree *Tree) int {
	t.Helper()
	h := 0
	id := tree.root
	for {
		n, err := tree.load(id)
		if err != nil {
			t.Fatalf("load(%d): %v", id, err)
		}
		if n.isLeaf() {
			return h
		}
		h++
		id = n.leftmostChild()
	}
}

// checkBalanced asserts every leaf sits at the same depth — the balance
// invariant a plain scan can't see.
func checkBalanced(t *testing.T, tree *Tree) {
	t.Helper()
	var depths []int
	var walk func(id pager.PageID, depth int)
	walk = func(id pager.PageID, depth int) {
		n, err := tree.load(id)
		if err != nil {
			t.Fatalf("load(%d): %v", id, err)
		}
		if n.isLeaf() {
			depths = append(depths, depth)
			return
		}
		walk(n.leftmostChild(), depth+1)
		for i := 0; i < n.numCells(); i++ {
			walk(n.child(i), depth+1)
		}
	}
	walk(tree.root, 0)
	for _, d := range depths {
		if d != depths[0] {
			t.Fatalf("unbalanced tree: leaf depths %v", depths)
		}
	}
}

func TestTreeEmpty(t *testing.T) {
	tree := newTree(t)

	_, found, err := tree.Get(42)
	assert.NoError(t, err)
	assert.False(t, found)

	c := tree.Cursor()
	defer c.Close()
	assert.False(t, c.Next())
	assert.NoError(t, c.Err())
}

func TestTreeInsertGetSorted(t *testing.T) {
	tree := newTree(t)
	keys := []int64{30, 10, 20, 5, 25}
	insertKeys(t, tree, keys, 12)

	verifyTree(t, tree, keys, 12)

	_, found, err := tree.Get(999)
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestTreeSingleLeafNoSplit(t *testing.T) {
	tree := newTree(t)
	keys := seqKeys(1, 6)
	insertKeys(t, tree, keys, 12)

	verifyTree(t, tree, keys, 12)
	assert.Equal(t, 0, treeHeight(t, tree))
}

func TestTreeLeafSplitGrowsHeight(t *testing.T) {
	tree := newTree(t)
	keys := seqKeys(1, 4) // 3 fat rows: 2 fit per leaf, so the 3rd forces a split
	insertKeys(t, tree, keys, 1500)

	verifyTree(t, tree, keys, 1500)
	checkBalanced(t, tree)
	if h := treeHeight(t, tree); h < 1 {
		t.Fatalf("height %d, want >= 1 after a split", h)
	}
}

func TestTreeSequentialInsert(t *testing.T) {
	tree := newTree(t)
	keys := seqKeys(1, 200)
	insertKeys(t, tree, keys, 1500)

	verifyTree(t, tree, keys, 1500)
	checkBalanced(t, tree)
	if h := treeHeight(t, tree); h < 1 {
		t.Fatalf("height %d, want a multi-level tree", h)
	}
}

func TestTreeReverseInsert(t *testing.T) {
	tree := newTree(t)
	keys := seqKeys(1, 200)
	for i := len(keys) - 1; i >= 0; i-- {
		if err := tree.Insert(keys[i], fatPayload(keys[i], 1500)); err != nil {
			t.Fatal(err)
		}
	}

	verifyTree(t, tree, keys, 1500)
	checkBalanced(t, tree)
}

func TestTreeRandomStress(t *testing.T) {
	tree := newTree(t)
	const n = 1000
	r := rand.New(rand.NewSource(1))
	keys := make([]int64, 0, n)
	for _, k := range r.Perm(n) {
		keys = append(keys, int64(k))
		if err := tree.Insert(int64(k), fatPayload(int64(k), 1500)); err != nil {
			t.Fatal(err)
		}
	}

	verifyTree(t, tree, keys, 1500)
	checkBalanced(t, tree)
	if h := treeHeight(t, tree); h < 2 {
		t.Fatalf("height %d, want >= 2 (a deep tree forces interior splits) for %d fat rows", h, n)
	}
}

func TestTreeReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	p, err := pager.Open(path)
	assert.NoError(t, err)
	tree, err := Create(p)
	assert.NoError(t, err)

	keys := seqKeys(1, 300)
	insertKeys(t, tree, keys, 1500)
	root := tree.Root()
	assert.NoError(t, p.Close()) // flush + close

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	tree2 := Open(p2, root)
	verifyTree(t, tree2, keys, 1500)
	checkBalanced(t, tree2)
}

// The root page id must never change, even as the tree grows several levels
// deep. Before Stage A a root split minted a new parent page and moved t.root
// onto it, orphaning the id the schema page persisted at CREATE time. Every
// other test reads back via the live tree.Root(), so none of them would have
// caught that — this one reopens using the id captured right after Create.
func TestTreeRootStableAcrossSplits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	p, err := pager.Open(path)
	assert.NoError(t, err)
	tree, err := Create(p)
	assert.NoError(t, err)

	rootAtCreate := tree.Root()

	keys := seqKeys(1, 400)
	for _, k := range keys {
		assert.NoError(t, tree.Insert(k, fatPayload(k, 1500)))
		if tree.Root() != rootAtCreate {
			t.Fatalf("root id changed to %d after inserting %d, want stable %d", tree.Root(), k, rootAtCreate)
		}
	}

	if h := treeHeight(t, tree); h < 2 {
		t.Fatalf("height %d, want >= 2 so the root split more than once", h)
	}
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	tree2 := Open(p2, rootAtCreate) // reopen at the id from CREATE, not a saved-off later id
	verifyTree(t, tree2, keys, 1500)
	checkBalanced(t, tree2)
}

func TestTreeRejectsOversizedRecord(t *testing.T) {
	tree := newTree(t)

	if err := tree.Insert(1, make([]byte, pager.PageSize)); err == nil {
		t.Fatal("Insert of a record larger than a page should error, got nil")
	}

	// a normal record still inserts and reads back
	assert.NoError(t, tree.Insert(2, payloadFor(2)))
	got, found, err := tree.Get(2)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.True(t, bytes.Equal(payloadFor(2), got))
}

// Inserting into a reopened tree dirties pages that were read from disk (not
// freshly allocated) — the path that silently mis-flushed when Get left page.ID
// at zero. This is the gap the other reopen tests missed by only reading.
func TestTreeInsertAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	p, err := pager.Open(path)
	assert.NoError(t, err)
	tree, err := Create(p)
	assert.NoError(t, err)
	insertKeys(t, tree, seqKeys(1, 150), 1500)
	root := tree.Root()
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	tree2 := Open(p2, root)
	insertKeys(t, tree2, seqKeys(150, 300), 1500)
	root = tree2.Root()
	assert.NoError(t, p2.Close())

	p3, err := pager.Open(path)
	assert.NoError(t, err)
	defer p3.Close()
	tree3 := Open(p3, root)
	verifyTree(t, tree3, seqKeys(1, 300), 1500)
	checkBalanced(t, tree3)
}
