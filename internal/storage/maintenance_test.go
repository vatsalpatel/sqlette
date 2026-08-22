package storage

// Stage D: a table write is no longer one write. Every test here ends by
// recomputing each index from a full table scan and demanding it matches what is
// actually stored, because a missed maintenance path produces no error at all —
// just an index that quietly disagrees with the table.

import (
	"bytes"
	"fmt"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func newTestTable(t *testing.T) (*Store, *Table) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	assert.NoError(t, err)
	tbl, err := s.CreateTable("t")
	assert.NoError(t, err)
	return s, tbl
}

func addIndex(t *testing.T, s *Store, tbl *Table, columns []int, unique bool) *Index {
	t.Helper()
	tree, err := createIndexTree(s.pager)
	assert.NoError(t, err)
	ix := newIndex(tree, columns, unique)
	tbl.AddIndex(ix)
	return ix
}

func describeKey(k []byte) string {
	tuple, err := record.Decode(k)
	if err != nil {
		return fmt.Sprintf("<undecodable %x>", k)
	}
	parts := make([]string, len(tuple))
	for i, v := range tuple {
		parts[i] = v.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// indexKeys reads what the index actually holds.
func indexKeys(t *testing.T, ix *Index) [][]byte {
	t.Helper()
	c := ix.tree.Cursor()
	defer c.Close()
	var got [][]byte
	for c.Next() {
		got = append(got, bytes.Clone(c.Key()))
	}
	assert.NoError(t, c.Err())
	return got
}

// assertIndexConsistent is the whole point of this stage. It rebuilds the index
// from scratch off a table scan and demands the stored one matches exactly.
// Asserting "three rows in, three entries out" would pass against an
// implementation that is wrong in precisely the way that matters, because it
// shares its assumptions with the code under test; a recomputation shares none.
func assertIndexConsistent(t *testing.T, tbl *Table, ix *Index) {
	t.Helper()

	var want [][]byte
	c := tbl.Scan()
	for c.Next() {
		want = append(want, indexKey(ix.keyCols(c.Row()), c.RowID()))
	}
	assert.NoError(t, c.Err())
	assert.NoError(t, c.Close())
	slices.SortFunc(want, compareIndexKeys)

	got := indexKeys(t, ix)

	for i := range min(len(want), len(got)) {
		if !bytes.Equal(want[i], got[i]) {
			t.Fatalf("index entry %d is %s, want %s (recomputed from a table scan)",
				i, describeKey(got[i]), describeKey(want[i]))
		}
	}
	if len(got) > len(want) {
		t.Fatalf("index holds %d entries, table implies %d — first extra is %s (a delete or update left it behind)",
			len(got), len(want), describeKey(got[len(want)]))
	}
	if len(got) < len(want) {
		t.Fatalf("index holds %d entries, table implies %d — first missing is %s (an insert or update did not add it)",
			len(got), len(want), describeKey(want[len(got)]))
	}
}

func tableRowCount(t *testing.T, tbl *Table) int {
	t.Helper()
	c := tbl.Scan()
	defer c.Close()
	n := 0
	for c.Next() {
		n++
	}
	assert.NoError(t, c.Err())
	return n
}

func personRow(name string, age int64) Row {
	return Row{values.NewInteger(0), values.NewText(name), values.NewInteger(age)}
}

func TestTableGetReturnsStoredRow(t *testing.T) {
	_, tbl := newTestTable(t)

	id, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)

	got, found, err := tbl.Get(id)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 0, compareTuples(personRow("ada", 36), got))

	_, found, err = tbl.Get(9999)
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestInsertMaintainsIndex(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	for _, name := range []string{"grace", "ada", "alan"} {
		_, err := tbl.Insert(personRow(name, 30))
		assert.NoError(t, err)
	}

	assert.Equal(t, 3, len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)
}

// The entry to remove is built from the row's *old* column values, which Delete
// only receives a rowid for. Without reading the pre-image first, the table row
// vanishes and the index entry stays behind, pointing at a rowid that no longer
// carries that value. Nothing errors; the index is just wrong from then on.
func TestDeleteRemovesTheEntryBuiltFromThePreImage(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	ada, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)
	_, err = tbl.Insert(personRow("grace", 45))
	assert.NoError(t, err)

	deleted, err := tbl.Delete(ada)
	assert.NoError(t, err)
	assert.True(t, deleted)

	assert.Equal(t, 1, len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)
}

// Same pre-image requirement, one step harder: the old entry has to go and the
// new one has to arrive. Skip the pre-image read and the row answers to both
// names forever.
func TestUpdateMovesEntryWhenKeyColumnChanges(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	id, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)

	updated, err := tbl.Update(id, personRow("Ada", 36))
	assert.NoError(t, err)
	assert.True(t, updated)

	assert.Equal(t, 1, len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)

	found, err := byName.HasPrefix([]values.Value{values.NewText("ada")})
	assert.NoError(t, err)
	assert.False(t, found)

	found, err = byName.HasPrefix([]values.Value{values.NewText("Ada")})
	assert.NoError(t, err)
	assert.True(t, found)
}

func TestUpdateLeavesIndexAloneWhenKeyColumnUnchanged(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	id, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)
	before := indexKeys(t, byName)

	// age is not indexed, so the key is identical afterwards
	updated, err := tbl.Update(id, personRow("ada", 37))
	assert.NoError(t, err)
	assert.True(t, updated)

	after := indexKeys(t, byName)
	assert.Equal(t, len(before), len(after))
	for i := range before {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatalf("entry %d changed to %s, want %s", i, describeKey(after[i]), describeKey(before[i]))
		}
	}
	assertIndexConsistent(t, tbl, byName)
}

func TestUpdateToIdenticalValues(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	id, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)

	updated, err := tbl.Update(id, personRow("ada", 36))
	assert.NoError(t, err)
	assert.True(t, updated)

	assert.Equal(t, 1, len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)
}

func TestEveryIndexIsMaintained(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)
	byAge := addIndex(t, s, tbl, []int{2}, false)

	ada, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)
	grace, err := tbl.Insert(personRow("grace", 45))
	assert.NoError(t, err)
	_, err = tbl.Insert(personRow("alan", 41))
	assert.NoError(t, err)

	_, err = tbl.Update(ada, personRow("Ada", 37))
	assert.NoError(t, err)
	_, err = tbl.Delete(grace)
	assert.NoError(t, err)

	assertIndexConsistent(t, tbl, byName)
	assertIndexConsistent(t, tbl, byAge)
}

func TestMissingRowMutationsTouchNoIndex(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)

	_, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)
	before := indexKeys(t, byName)

	deleted, err := tbl.Delete(9999)
	assert.NoError(t, err)
	assert.False(t, deleted)

	updated, err := tbl.Update(9999, personRow("nobody", 0))
	assert.NoError(t, err)
	assert.False(t, updated)

	assert.Equal(t, len(before), len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)
}

// The check has to happen before the first write, so a rejected statement leaves
// nothing behind. Writing the row and then discovering the conflict would leave
// a table row that the index has no entry for.
func TestUniqueRejectsDuplicateAndWritesNothing(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, true)

	_, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)

	if _, err := tbl.Insert(personRow("ada", 99)); err == nil {
		t.Fatal("inserting a duplicate into a UNIQUE index should error, got nil")
	}

	assert.Equal(t, 1, tableRowCount(t, tbl))
	assert.Equal(t, 1, len(indexKeys(t, byName)))
	assertIndexConsistent(t, tbl, byName)
}

// SQLite lets a unique index hold any number of NULLs, on the grounds that two
// NULLs are not known to be equal.
func TestUniqueAllowsRepeatedNulls(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, true)

	for range 3 {
		_, err := tbl.Insert(Row{values.NewInteger(0), values.NewNull(), values.NewInteger(1)})
		assert.NoError(t, err)
	}

	assert.Equal(t, 3, tableRowCount(t, tbl))
	assertIndexConsistent(t, tbl, byName)
}

// The trap: a row being updated is already in the index, so a naive probe finds
// the row's own entry and rejects a statement that conflicts with nothing. Both
// of these must succeed.
func TestUniqueUpdateDoesNotConflictWithItself(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, true)

	id, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)

	// a non-key column changes; the unique key is untouched
	updated, err := tbl.Update(id, personRow("ada", 37))
	assert.NoError(t, err)
	assert.True(t, updated)

	// the key column is reassigned its own current value
	updated, err = tbl.Update(id, personRow("ada", 38))
	assert.NoError(t, err)
	assert.True(t, updated)

	assertIndexConsistent(t, tbl, byName)
}

func TestUniqueRejectsUpdateOntoAnotherRow(t *testing.T) {
	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, true)

	_, err := tbl.Insert(personRow("ada", 36))
	assert.NoError(t, err)
	grace, err := tbl.Insert(personRow("grace", 45))
	assert.NoError(t, err)

	if _, err := tbl.Update(grace, personRow("ada", 45)); err == nil {
		t.Fatal("updating onto another row's UNIQUE value should error, got nil")
	}

	row, found, err := tbl.Get(grace)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 0, compareTuples(personRow("grace", 45), row))
	assertIndexConsistent(t, tbl, byName)
}

// Uniqueness is decided in value space, so INTEGER 1 and REAL 1.0 collide even
// though their keys differ byte for byte.
func TestUniqueTreatsIntegerAndRealAsTheSameValue(t *testing.T) {
	s, tbl := newTestTable(t)
	byNum := addIndex(t, s, tbl, []int{0}, true)

	_, err := tbl.Insert(Row{values.NewInteger(1), values.NewText("a"), values.NewInteger(0)})
	assert.NoError(t, err)

	if _, err := tbl.Insert(Row{values.NewReal(1.0), values.NewText("b"), values.NewInteger(0)}); err == nil {
		t.Fatal("REAL 1.0 should collide with INTEGER 1 in a UNIQUE index, got nil")
	}

	assert.Equal(t, 1, tableRowCount(t, tbl))
	assertIndexConsistent(t, tbl, byNum)
}

// The torture test. A long random stream of inserts, updates and deletes, with
// heavy value collisions so the rowid suffix is constantly in play, checked
// periodically so a failure localises instead of surfacing at op 5000.
func TestRandomOperationStreamKeepsIndexesConsistent(t *testing.T) {
	const ops = 5000

	s, tbl := newTestTable(t)
	byName := addIndex(t, s, tbl, []int{1}, false)
	byAge := addIndex(t, s, tbl, []int{2}, false)

	r := rand.New(rand.NewSource(7))
	names := []string{"ada", "alan", "grace", "linus"} // collide often on purpose
	randomRow := func() Row {
		return personRow(names[r.Intn(len(names))], int64(r.Intn(5)))
	}

	var ids []int64
	for op := range ops {
		switch {
		case len(ids) == 0 || r.Intn(100) < 45:
			id, err := tbl.Insert(randomRow())
			assert.NoError(t, err)
			ids = append(ids, id)
		case r.Intn(100) < 60:
			i := r.Intn(len(ids))
			updated, err := tbl.Update(ids[i], randomRow())
			assert.NoError(t, err)
			assert.True(t, updated)
		default:
			i := r.Intn(len(ids))
			deleted, err := tbl.Delete(ids[i])
			assert.NoError(t, err)
			assert.True(t, deleted)
			ids[i] = ids[len(ids)-1]
			ids = ids[:len(ids)-1]
		}

		if op%500 == 0 {
			assertIndexConsistent(t, tbl, byName)
			assertIndexConsistent(t, tbl, byAge)
		}
	}

	assertIndexConsistent(t, tbl, byName)
	assertIndexConsistent(t, tbl, byAge)
}
