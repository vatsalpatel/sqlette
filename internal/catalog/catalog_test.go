package catalog_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/catalog"
)

func sampleTable() *catalog.Table {
	return &catalog.Table{
		Name: "users",
		Column: []catalog.Column{
			{Name: "id", Type: "INT", PrimaryKey: true},
			{Name: "name", Type: "TEXT", NotNull: true},
			{Name: "age", Type: "INT"},
		},
	}
}

func TestNewIsEmpty(t *testing.T) {
	c := catalog.New()
	assert.Equal(t, 0, len(c.Tables))
}

func TestCreateAndGet(t *testing.T) {
	c := catalog.New()
	tbl := sampleTable()

	assert.NoError(t, c.Create(tbl))

	got, ok := c.Get("users")
	assert.True(t, ok)
	assert.DeepEqual(t, tbl, got)
}

func TestGetUnknown(t *testing.T) {
	c := catalog.New()

	got, ok := c.Get("missing")
	assert.False(t, ok)
	assert.True(t, got == nil)
}

func TestColumnIndex(t *testing.T) {
	tbl := sampleTable()
	tests := []struct {
		name string
		col  string
		idx  int
		ok   bool
	}{
		{"first column", "id", 0, true},
		{"middle column", "name", 1, true},
		{"last column", "age", 2, true},
		{"unknown column", "nope", -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, ok := tbl.ColumnIndex(tt.col)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.idx, idx)
		})
	}
}

func TestColumnIndexEmptyTable(t *testing.T) {
	tbl := &catalog.Table{Name: "empty"}

	idx, ok := tbl.ColumnIndex("anything")
	assert.False(t, ok)
	assert.Equal(t, -1, idx)
}

func TestGetIsCaseInsensitive(t *testing.T) {
	c := catalog.New()
	tbl := &catalog.Table{
		Name: "Users",
		Column: []catalog.Column{
			{Name: "id", Type: "INT", PrimaryKey: true},
		},
	}
	assert.NoError(t, c.Create(tbl))

	for _, name := range []string{"users", "USERS", "uSeRs", "Users"} {
		t.Run(name, func(t *testing.T) {
			got, ok := c.Get(name)
			assert.True(t, ok)
			assert.DeepEqual(t, tbl, got)
		})
	}
}

func TestCreateDuplicateIsCaseInsensitive(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{Name: "users"}))

	err := c.Create(&catalog.Table{Name: "USERS"})
	assert.ErrorContains(t, err, "already exists")
}

func TestColumnIndexIsCaseInsensitive(t *testing.T) {
	tbl := sampleTable()
	tests := []struct {
		col string
		idx int
	}{
		{"ID", 0},
		{"Id", 0},
		{"NAME", 1},
		{"Age", 2},
	}
	for _, tt := range tests {
		t.Run(tt.col, func(t *testing.T) {
			idx, ok := tbl.ColumnIndex(tt.col)
			assert.True(t, ok)
			assert.Equal(t, tt.idx, idx)
		})
	}
}
