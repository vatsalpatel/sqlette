package catalog_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

func TestSchemaRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		tables []*catalog.Table
	}{
		{"empty catalog", nil},
		{"single column", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{
				{Name: "id", Type: "INTEGER", PrimaryKey: true},
			}},
		}},
		{"all flag combos", []*catalog.Table{
			{Name: "users", RootPage: 5, Columns: []catalog.Column{
				{Name: "id", Type: "INTEGER", PrimaryKey: true},
				{Name: "email", Type: "TEXT", NotNull: true},
				{Name: "handle", Type: "TEXT", PrimaryKey: true, NotNull: true},
				{Name: "age", Type: "INTEGER"},
			}},
		}},
		{"multiple tables", []*catalog.Table{
			{Name: "a", RootPage: 2, Columns: []catalog.Column{{Name: "x", Type: "INT"}}},
			{Name: "b", RootPage: 3, Columns: []catalog.Column{{Name: "y", Type: "REAL"}}},
			{Name: "c", RootPage: 4, Columns: []catalog.Column{{Name: "z", Type: "BLOB"}}},
		}},
		{"empty strings", []*catalog.Table{
			{Name: "t", RootPage: 2, Columns: []catalog.Column{{Name: "", Type: ""}}},
		}},
		{"large root page", []*catalog.Table{
			{Name: "big", RootPage: 300000, Columns: []catalog.Column{{Name: "c", Type: "INT"}}},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := catalog.New()
			for _, tbl := range tc.tables {
				assert.NoError(t, c.Create(tbl))
			}

			c2 := catalog.New()
			assert.NoError(t, c2.Unmarshal(c.Marshal()))
			assert.DeepEqual(t, c.Tables, c2.Tables)
		})
	}
}

// The schema lives inside a 4 KB page, so Unmarshal must ignore the zero
// padding that follows the blob.
func TestUnmarshalIgnoresTrailingBytes(t *testing.T) {
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
}

func TestUnmarshalRejectsTruncatedInput(t *testing.T) {
	c := catalog.New()
	assert.NoError(t, c.Create(&catalog.Table{
		Name: "users", RootPage: 2,
		Columns: []catalog.Column{
			{Name: "id", Type: "INTEGER", PrimaryKey: true},
			{Name: "name", Type: "TEXT"},
		},
	}))
	full := c.Marshal()

	for n := 0; n < len(full); n++ {
		checkTruncated(t, full[:n], n)
	}
}

func checkTruncated(t *testing.T, prefix []byte, n int) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unmarshal(len %d) panicked, want a returned error: %v", n, r)
		}
	}()
	if err := catalog.New().Unmarshal(prefix); err == nil {
		t.Errorf("Unmarshal(len %d) returned nil, want error", n)
	}
}
