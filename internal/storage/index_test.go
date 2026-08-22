package storage

// Internal tests: index keys, their comparator, and the Index verbs are all
// unexported, and they are the whole substance of this layer.

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/btree"
	"github.com/vatsalpatel/sqlette/internal/pager"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func newTestIndex(t *testing.T, columns []int, unique bool) *Index {
	t.Helper()
	p, err := pager.Open(filepath.Join(t.TempDir(), "index.db"))
	assert.NoError(t, err)
	tree, err := createIndexTree(p)
	assert.NoError(t, err)
	return newIndex(tree, columns, unique)
}

// tupleOf is the key an entry is expected to encode to, used to compute the
// expected ordering independently of the tree.
func tupleOf(cols []values.Value, rowid int64) []values.Value {
	return append(slices.Clone(cols), values.NewInteger(rowid))
}

// drainRowIDs reads the rowid suffix off every entry a cursor yields. Rowids are
// unique, so a sequence of them pins the exact entries and their exact order.
func drainRowIDs(t *testing.T, c *btree.Cursor) []int64 {
	t.Helper()
	defer c.Close()
	var out []int64
	for c.Next() {
		tuple, err := record.Decode(c.Key())
		assert.NoError(t, err)
		if len(tuple) == 0 {
			t.Fatal("decoded an empty index key — the entry was written through the table façade, not InsertKey")
		}
		out = append(out, tuple[len(tuple)-1].Int)
	}
	assert.NoError(t, c.Err())
	return out
}

func scanRowIDs(t *testing.T, ix *Index) []int64 {
	t.Helper()
	return drainRowIDs(t, ix.tree.Cursor())
}

func assertRowIDs(t *testing.T, want, got []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("scan returned %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry[%d] has rowid %d, want %d", i, got[i], want[i])
		}
	}
}

// randomValue draws from all five storage classes over a deliberately tiny
// range, so ties are common — a tie is what forces the rowid suffix to do its
// job, and a cross-class tie is what exercises the rank ordering.
func randomValue(r *rand.Rand) values.Value {
	switch r.Intn(5) {
	case 0:
		return values.NewNull()
	case 1:
		return values.NewInteger(int64(r.Intn(7) - 3))
	case 2:
		return values.NewReal(float64(r.Intn(7)-3) / 2)
	case 3:
		return values.NewText(string(rune('a' + r.Intn(4))))
	default:
		return values.NewBlob([]byte{byte(r.Intn(4))})
	}
}

// The tree must lay entries out in exactly the order the comparator dictates,
// across every storage class, with the rowid breaking every tie. The expected
// order is computed with compareTuples in memory: this asserts the tree agrees
// with the comparator, while the tests below pin the comparator's own rules.
func TestIndexOrdersByValueSemantics(t *testing.T) {
	const n = 2000

	ix := newTestIndex(t, []int{0}, false)
	r := rand.New(rand.NewSource(1))

	type entry struct {
		cols  []values.Value
		rowid int64
	}
	entries := make([]entry, 0, n)
	for i := range n {
		e := entry{cols: []values.Value{randomValue(r)}, rowid: int64(i + 1)}
		entries = append(entries, e)
		assert.NoError(t, ix.Insert(e.cols, e.rowid))
	}

	want := slices.Clone(entries)
	slices.SortFunc(want, func(a, b entry) int {
		return compareTuples(tupleOf(a.cols, a.rowid), tupleOf(b.cols, b.rowid))
	})

	wantIDs := make([]int64, len(want))
	for i, e := range want {
		wantIDs[i] = e.rowid
	}
	assertRowIDs(t, wantIDs, scanRowIDs(t, ix))
}

// Rows sharing a column value are exactly what a non-unique index exists for.
// They differ only in the suffix, so they come back in rowid order.
func TestIndexDuplicateValuesOrderByRowID(t *testing.T) {
	ix := newTestIndex(t, []int{0}, false)

	same := Row{values.NewText("ada")}
	for _, rowid := range []int64{40, 10, 30, 20} {
		assert.NoError(t, ix.Insert(same, rowid))
	}

	assertRowIDs(t, []int64{10, 20, 30, 40}, scanRowIDs(t, ix))
}

func TestIndexNullsSortFirst(t *testing.T) {
	ix := newTestIndex(t, []int{0}, false)

	assert.NoError(t, ix.Insert(Row{values.NewText("ada")}, 1))
	assert.NoError(t, ix.Insert(Row{values.NewNull()}, 2))
	assert.NoError(t, ix.Insert(Row{values.NewInteger(5)}, 3))
	assert.NoError(t, ix.Insert(Row{values.NewBlob([]byte{0})}, 4))

	// NULL < numeric < text < blob
	assertRowIDs(t, []int64{2, 3, 1, 4}, scanRowIDs(t, ix))
}

// INTEGER 1 and REAL 1.0 compare equal but encode differently, so "compares
// equal" does not imply "same bytes". The invariant that actually holds is the
// one the rowid suffix provides: distinct rows never produce equal keys, so both
// entries coexist and each can be removed on its own.
func TestIndexIntegerAndRealCompareEqualButEncodeDifferently(t *testing.T) {
	one := values.NewInteger(1)
	oneReal := values.NewReal(1.0)

	assert.Equal(t, 0, values.Compare(one, oneReal))
	if bytes.Equal(indexKey([]values.Value{one}, 7), indexKey([]values.Value{oneReal}, 7)) {
		t.Fatal("INTEGER and REAL encoded identically; the record codec should tag them apart")
	}

	ix := newTestIndex(t, []int{0}, false)
	assert.NoError(t, ix.Insert(Row{one}, 1))
	assert.NoError(t, ix.Insert(Row{oneReal}, 2))
	assertRowIDs(t, []int64{1, 2}, scanRowIDs(t, ix))

	deleted, err := ix.Delete(Row{one}, 1)
	assert.NoError(t, err)
	assert.True(t, deleted)
	assertRowIDs(t, []int64{2}, scanRowIDs(t, ix))
}

// A uniqueness probe works in value space, so it treats 1 and 1.0 as the same
// value even though their keys differ byte for byte. That matches SQLite.
func TestIndexHasPrefixIsNumericNotBytewise(t *testing.T) {
	ix := newTestIndex(t, []int{0}, true)
	assert.NoError(t, ix.Insert(Row{values.NewInteger(1)}, 1))

	found, err := ix.HasPrefix([]values.Value{values.NewReal(1.0)})
	assert.NoError(t, err)
	assert.True(t, found)
}

// The index keys off its own columns, not off whatever the row happens to hold
// first, and Delete has to rebuild the same key from the same row.
func TestIndexUsesItsOwnColumns(t *testing.T) {
	ix := newTestIndex(t, []int{1}, false)

	// column 1 is the indexed one; columns 0 and 2 must not affect ordering
	assert.NoError(t, ix.Insert(Row{values.NewInteger(99), values.NewText("c"), values.NewText("x")}, 1))
	assert.NoError(t, ix.Insert(Row{values.NewInteger(11), values.NewText("a"), values.NewText("y")}, 2))
	assert.NoError(t, ix.Insert(Row{values.NewInteger(55), values.NewText("b"), values.NewText("z")}, 3))

	assertRowIDs(t, []int64{2, 3, 1}, scanRowIDs(t, ix))

	deleted, err := ix.Delete(Row{values.NewInteger(11), values.NewText("a"), values.NewText("y")}, 2)
	assert.NoError(t, err)
	assert.True(t, deleted)
	assertRowIDs(t, []int64{3, 1}, scanRowIDs(t, ix))
}

func TestIndexDeleteMissingEntry(t *testing.T) {
	ix := newTestIndex(t, []int{0}, false)
	assert.NoError(t, ix.Insert(Row{values.NewText("ada")}, 1))

	// right value, wrong rowid
	deleted, err := ix.Delete(Row{values.NewText("ada")}, 2)
	assert.NoError(t, err)
	assert.False(t, deleted)

	// right rowid, wrong value
	deleted, err = ix.Delete(Row{values.NewText("alan")}, 1)
	assert.NoError(t, err)
	assert.False(t, deleted)

	assertRowIDs(t, []int64{1}, scanRowIDs(t, ix))
}

// A two-column index probed on its leading column only. The missing column is
// padded with NULL, which is a correct lower bound because NULL ranks below
// every other storage class — this is the case the planner hits in Stage F.
func TestIndexSeekPrefixShorterThanIndex(t *testing.T) {
	ix := newTestIndex(t, []int{0, 1}, false)

	assert.NoError(t, ix.Insert(Row{values.NewText("a"), values.NewInteger(2)}, 1))
	assert.NoError(t, ix.Insert(Row{values.NewText("b"), values.NewInteger(1)}, 2))
	assert.NoError(t, ix.Insert(Row{values.NewText("a"), values.NewNull()}, 3))
	assert.NoError(t, ix.Insert(Row{values.NewText("a"), values.NewInteger(1)}, 4))
	assert.NoError(t, ix.Insert(Row{values.NewText("c"), values.NewInteger(1)}, 5))

	// within 'a': NULL first, then 1, then 2. Then 'b', then 'c'.
	assertRowIDs(t, []int64{3, 4, 1, 2, 5}, scanRowIDs(t, ix))

	got := drainRowIDs(t, ix.SeekPrefix([]values.Value{values.NewText("a")}))
	assertRowIDs(t, []int64{3, 4, 1, 2, 5}, got)

	got = drainRowIDs(t, ix.SeekPrefix([]values.Value{values.NewText("b")}))
	assertRowIDs(t, []int64{2, 5}, got)
}

func TestIndexSeekPrefixLandsOnNextGreater(t *testing.T) {
	ix := newTestIndex(t, []int{0}, false)
	assert.NoError(t, ix.Insert(Row{values.NewInteger(10)}, 1))
	assert.NoError(t, ix.Insert(Row{values.NewInteger(30)}, 2))

	got := drainRowIDs(t, ix.SeekPrefix([]values.Value{values.NewInteger(20)}))
	assertRowIDs(t, []int64{2}, got)

	got = drainRowIDs(t, ix.SeekPrefix([]values.Value{values.NewInteger(99)}))
	assertRowIDs(t, nil, got)
}

func TestIndexHasPrefix(t *testing.T) {
	ix := newTestIndex(t, []int{0}, true)

	found, err := ix.HasPrefix([]values.Value{values.NewText("ada")})
	assert.NoError(t, err)
	assert.False(t, found) // empty index

	assert.NoError(t, ix.Insert(Row{values.NewText("ada")}, 1))
	assert.NoError(t, ix.Insert(Row{values.NewText("grace")}, 2))

	for _, c := range []struct {
		name  string
		probe values.Value
		want  bool
	}{
		{"present, first", values.NewText("ada"), true},
		{"present, last", values.NewText("grace"), true},
		{"absent, below everything", values.NewText("aa"), false},
		{"absent, between entries", values.NewText("b"), false},
		{"absent, past the end", values.NewText("zz"), false},
		{"absent, different storage class", values.NewInteger(1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, err := ix.HasPrefix([]values.Value{c.probe})
			assert.NoError(t, err)
			assert.Equal(t, c.want, found)
		})
	}
}

// indexKey appends the rowid onto the caller's columns. Appending directly would
// write into the caller's backing array whenever it has spare capacity, which is
// silent and only shows up when a caller reuses a buffer.
func TestIndexKeyDoesNotWriteIntoCallersSlice(t *testing.T) {
	backing := make([]values.Value, 2)
	backing[0] = values.NewText("ada")
	backing[1] = values.NewText("sentinel")

	cols := backing[:1] // len 1, cap 2
	indexKey(cols, 42)

	if values.Compare(backing[1], values.NewText("sentinel")) != 0 {
		t.Fatalf("indexKey clobbered the caller's backing array: %v", backing[1])
	}
}

func TestIndexReopenKeepsOrdering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	p, err := pager.Open(path)
	assert.NoError(t, err)
	tree, err := createIndexTree(p)
	assert.NoError(t, err)
	ix := newIndex(tree, []int{0}, false)

	r := rand.New(rand.NewSource(2))
	for i := range 500 {
		assert.NoError(t, ix.Insert(Row{randomValue(r)}, int64(i+1)))
	}
	before := scanRowIDs(t, ix)
	root := ix.Root()
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	reopened := newIndex(openIndexTree(p2, root), []int{0}, false)
	assertRowIDs(t, before, scanRowIDs(t, reopened))
}
