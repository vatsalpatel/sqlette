package catalog_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

func TestSchemaRoundTripWithIndexes(t *testing.T) {
	cases := []struct {
		name    string
		tables  []*catalog.Table
		indexes []*catalog.Index
	}{
		{"no indexes", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{{Name: "a", Type: "INT"}}},
		}, nil},
		{"one index", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{{Name: "a", Type: "INT"}}},
		}, []*catalog.Index{
			{Name: "idx_a", Table: "t", Columns: []string{"a"}, RootPage: 3},
		}},
		{"unique index", []*catalog.Table{
			{Name: "users", RootPage: 2, Columns: []catalog.Column{{Name: "email", Type: "TEXT"}}},
		}, []*catalog.Index{
			{Name: "idx_email", Table: "users", Columns: []string{"email"}, Unique: true, RootPage: 4},
		}},
		{"multi column index", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{
				{Name: "a", Type: "INT"}, {Name: "b", Type: "TEXT"}, {Name: "c", Type: "REAL"},
			}},
		}, []*catalog.Index{
			{Name: "idx_abc", Table: "t", Columns: []string{"a", "b", "c"}, RootPage: 3},
		}},
		{"several indexes across several tables", []*catalog.Table{
			{Name: "a", RootPage: 2, Columns: []catalog.Column{{Name: "x", Type: "INT"}}},
			{Name: "b", RootPage: 3, Columns: []catalog.Column{{Name: "y", Type: "TEXT"}}},
		}, []*catalog.Index{
			{Name: "i1", Table: "a", Columns: []string{"x"}, RootPage: 4},
			{Name: "i2", Table: "b", Columns: []string{"y"}, Unique: true, RootPage: 5},
			{Name: "i3", Table: "a", Columns: []string{"x"}, RootPage: 6},
		}},
		{"large root page", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{{Name: "a", Type: "INT"}}},
		}, []*catalog.Index{
			{Name: "big", Table: "t", Columns: []string{"a"}, RootPage: 300000},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := catalog.New()
			for _, tbl := range tc.tables {
				assert.NoError(t, c.Create(tbl))
			}
			for _, ix := range tc.indexes {
				assert.NoError(t, c.CreateIndex(ix))
			}

			c2 := catalog.New()
			assert.NoError(t, c2.Unmarshal(c.Marshal()))
			assert.DeepEqual(t, c.Tables, c2.Tables)
			assert.DeepEqual(t, c.Indexes, c2.Indexes)
		})
	}
}

// A schema page written before M6 has no index section at all, just zero padding
// where the count now lives. Reading a count out of zeros gives zero indexes, so
// old files keep loading with no version byte anywhere in the format.
func TestUnmarshalTreatsZeroPaddingAsNoIndexes(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{
		Name: "t", RootPage: 2,
		Columns: []catalog.Column{{Name: "id", Type: "INT"}},
	}))

	page := make([]byte, pager.PageSize)
	copy(page, c.Marshal())

	c2 := catalog.New()
	assert.NoError(t, c2.Unmarshal(page))
	assert.DeepEqual(t, c.Tables, c2.Tables)
	assert.Equal(t, 0, len(c2.Indexes))
}

// Every prefix of a valid blob has to come back as an error rather than a panic
// or a half-built catalog — the index section included.
func TestUnmarshalRejectsTruncatedIndexSection(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{
		Name: "users", RootPage: 2,
		Columns: []catalog.Column{{Name: "email", Type: "TEXT"}},
	}))
	assert.NoError(t, c.CreateIndex(&catalog.Index{
		Name: "idx_email", Table: "users", Columns: []string{"email"}, Unique: true, RootPage: 3,
	}))
	full := c.Marshal()

	for n := 1; n < len(full); n++ {
		checkTruncated(t, full[:n], n)
	}
}

func TestCreateIndexRejectsDuplicateName(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{
		Name: "t", RootPage: 2, Columns: []catalog.Column{{Name: "a", Type: "INT"}},
	}))
	assert.NoError(t, c.CreateIndex(&catalog.Index{
		Name: "idx", Table: "t", Columns: []string{"a"}, RootPage: 3,
	}))

	if err := c.CreateIndex(&catalog.Index{
		Name: "idx", Table: "t", Columns: []string{"a"}, RootPage: 4,
	}); err == nil {
		t.Fatal("creating a second index named idx should error, got nil")
	}
}

func TestIndexesFor(t *testing.T) {
	c := catalog.New()
	for _, name := range []string{"a", "b"} {
		assert.NoError(t, c.Create(&catalog.Table{
			Name: name, RootPage: 2, Columns: []catalog.Column{{Name: "x", Type: "INT"}},
		}))
	}
	for _, ix := range []*catalog.Index{
		{Name: "i1", Table: "a", Columns: []string{"x"}, RootPage: 4},
		{Name: "i2", Table: "b", Columns: []string{"x"}, RootPage: 5},
		{Name: "i3", Table: "a", Columns: []string{"x"}, RootPage: 6},
	} {
		assert.NoError(t, c.CreateIndex(ix))
	}

	assert.Equal(t, 2, len(c.IndexesFor("a")))
	assert.Equal(t, 1, len(c.IndexesFor("b")))
	assert.Equal(t, 0, len(c.IndexesFor("nosuchtable")))
}

// Table names are stored lowercased, so index lookups should not be the one
// place in the catalog where case suddenly matters.
func TestIndexLookupIsCaseInsensitive(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{
		Name: "T", RootPage: 2, Columns: []catalog.Column{{Name: "a", Type: "INT"}},
	}))
	assert.NoError(t, c.CreateIndex(&catalog.Index{
		Name: "IDX", Table: "T", Columns: []string{"a"}, RootPage: 3,
	}))

	_, ok := c.GetIndex("idx")
	assert.True(t, ok)
	assert.Equal(t, 1, len(c.IndexesFor("t")))
}
