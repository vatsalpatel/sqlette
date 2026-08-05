package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func row(vals ...values.Value) storage.Row {
	return storage.Row(vals)
}

func newStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	assert.NoError(t, err)
	return s
}

func createTable(t *testing.T, s *storage.Store, name string) *storage.Table {
	t.Helper()
	tbl, err := s.CreateTable(name)
	assert.NoError(t, err)
	return tbl
}

func insert(t *testing.T, tbl *storage.Table, r storage.Row) int64 {
	t.Helper()
	id, err := tbl.Insert(r)
	assert.NoError(t, err)
	return id
}

func TestInsertAssignsRowIDs(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")

	assert.Equal(t, int64(1), insert(t, tbl, row(values.NewInteger(1), values.NewText("ada"))))
	assert.Equal(t, int64(2), insert(t, tbl, row(values.NewInteger(2), values.NewText("alan"))))
	assert.Equal(t, int64(3), insert(t, tbl, row(values.NewInteger(3), values.NewText("grace"))))
}

func TestScanWalksInsertOrder(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1), values.NewText("ada")))
	insert(t, tbl, row(values.NewInteger(2), values.NewText("alan")))

	cur := tbl.Scan()
	defer cur.Close()

	assert.True(t, cur.Next())
	assert.DeepEqual(t, row(values.NewInteger(1), values.NewText("ada")), cur.Row())

	assert.True(t, cur.Next())
	assert.DeepEqual(t, row(values.NewInteger(2), values.NewText("alan")), cur.Row())

	assert.False(t, cur.Next())
	assert.NoError(t, cur.Err())
}

func TestScanEmptyTable(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "empty")

	cur := tbl.Scan()
	defer cur.Close()

	assert.False(t, cur.Next())
}

func TestStoreLookupReturnsSameTable(t *testing.T) {
	s := newStore(t)
	insert(t, createTable(t, s, "users"), row(values.NewInteger(1), values.NewText("ada")))

	tbl, ok := s.Table("users")
	assert.True(t, ok)

	cur := tbl.Scan()
	defer cur.Close()
	assert.True(t, cur.Next())
	assert.DeepEqual(t, row(values.NewInteger(1), values.NewText("ada")), cur.Row())
	assert.False(t, cur.Next())
}

func TestStoreUnknownTable(t *testing.T) {
	s := newStore(t)

	_, ok := s.Table("missing")
	assert.False(t, ok)
}

func TestStoreTablesAreIndependent(t *testing.T) {
	s := newStore(t)
	users := createTable(t, s, "users")
	posts := createTable(t, s, "posts")

	insert(t, users, row(values.NewInteger(1)))

	cur := posts.Scan()
	defer cur.Close()
	assert.False(t, cur.Next())
}

func TestScanExposesRowIDs(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1), values.NewText("ada")))
	insert(t, tbl, row(values.NewInteger(2), values.NewText("alan")))

	cur := tbl.Scan()
	defer cur.Close()

	assert.True(t, cur.Next())
	assert.Equal(t, int64(1), cur.RowID())
	assert.True(t, cur.Next())
	assert.Equal(t, int64(2), cur.RowID())
	assert.False(t, cur.Next())
}

func TestDeleteRemovesRow(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1), values.NewText("ada")))
	insert(t, tbl, row(values.NewInteger(2), values.NewText("alan")))
	insert(t, tbl, row(values.NewInteger(3), values.NewText("grace")))

	ok, err := tbl.Delete(2)
	assert.NoError(t, err)
	assert.True(t, ok)

	cur := tbl.Scan()
	defer cur.Close()

	assert.True(t, cur.Next())
	assert.Equal(t, int64(1), cur.RowID())
	assert.DeepEqual(t, row(values.NewInteger(1), values.NewText("ada")), cur.Row())

	assert.True(t, cur.Next())
	assert.Equal(t, int64(3), cur.RowID())
	assert.DeepEqual(t, row(values.NewInteger(3), values.NewText("grace")), cur.Row())

	assert.False(t, cur.Next())
}

func TestDeleteMissingRow(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1)))

	ok, err := tbl.Delete(999)
	assert.NoError(t, err)
	assert.False(t, ok)

	cur := tbl.Scan()
	defer cur.Close()
	assert.True(t, cur.Next())
	assert.Equal(t, int64(1), cur.RowID())
	assert.False(t, cur.Next())
}

func TestUpdateChangesRow(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1), values.NewText("ada")))
	insert(t, tbl, row(values.NewInteger(2), values.NewText("alan")))

	ok, err := tbl.Update(2, row(values.NewInteger(2), values.NewText("alan turing")))
	assert.NoError(t, err)
	assert.True(t, ok)

	cur := tbl.Scan()
	defer cur.Close()

	assert.True(t, cur.Next())
	assert.DeepEqual(t, row(values.NewInteger(1), values.NewText("ada")), cur.Row())

	assert.True(t, cur.Next())
	assert.Equal(t, int64(2), cur.RowID())
	assert.DeepEqual(t, row(values.NewInteger(2), values.NewText("alan turing")), cur.Row())

	assert.False(t, cur.Next())
}

func TestUpdateMissingRow(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1)))

	ok, err := tbl.Update(999, row(values.NewInteger(9)))
	assert.NoError(t, err)
	assert.False(t, ok)
}

// Deleting the highest rowid must not lower nextID: the next insert gets a fresh
// id, never a reused one, within a session.
func TestRowIDsNotReusedAfterDelete(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "users")
	insert(t, tbl, row(values.NewInteger(1)))
	insert(t, tbl, row(values.NewInteger(2)))
	assert.Equal(t, int64(3), insert(t, tbl, row(values.NewInteger(3))))

	ok, err := tbl.Delete(3)
	assert.NoError(t, err)
	assert.True(t, ok)

	assert.Equal(t, int64(4), insert(t, tbl, row(values.NewInteger(4))))
}
