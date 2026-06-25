package storage_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func row(vals ...values.Value) storage.Row {
	return storage.Row(vals)
}

func TestInsertAssignsRowIDs(t *testing.T) {
	s := storage.New()
	tbl := s.CreateTable("users")

	assert.Equal(t, int64(1), tbl.Insert(row(values.NewInteger(1), values.NewText("ada"))))
	assert.Equal(t, int64(2), tbl.Insert(row(values.NewInteger(2), values.NewText("alan"))))
	assert.Equal(t, int64(3), tbl.Insert(row(values.NewInteger(3), values.NewText("grace"))))
}

func TestScanWalksInsertOrder(t *testing.T) {
	s := storage.New()
	tbl := s.CreateTable("users")
	tbl.Insert(row(values.NewInteger(1), values.NewText("ada")))
	tbl.Insert(row(values.NewInteger(2), values.NewText("alan")))

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
	s := storage.New()
	tbl := s.CreateTable("empty")

	cur := tbl.Scan()
	defer cur.Close()

	assert.False(t, cur.Next())
}

func TestStoreLookupReturnsSameTable(t *testing.T) {
	s := storage.New()
	s.CreateTable("users").Insert(row(values.NewInteger(1), values.NewText("ada")))

	tbl, ok := s.Table("users")
	assert.True(t, ok)

	cur := tbl.Scan()
	defer cur.Close()
	assert.True(t, cur.Next())
	assert.DeepEqual(t, row(values.NewInteger(1), values.NewText("ada")), cur.Row())
	assert.False(t, cur.Next())
}

func TestStoreUnknownTable(t *testing.T) {
	s := storage.New()

	_, ok := s.Table("missing")
	assert.False(t, ok)
}

func TestStoreTablesAreIndependent(t *testing.T) {
	s := storage.New()
	users := s.CreateTable("users")
	posts := s.CreateTable("posts")

	users.Insert(row(values.NewInteger(1)))

	cur := posts.Scan()
	defer cur.Close()
	assert.False(t, cur.Next())
}
