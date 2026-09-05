package exec

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
)

// A scope is the executor's whole idea of what a row contains. Until M7 that
// was a *catalog.Table, which cannot describe a row assembled from two tables,
// so these tests are written against the shape joins will actually produce
// rather than the single-table shape the engine can currently build.
var joined = Scope{
	{Table: "users", Name: "id"},
	{Table: "users", Name: "name"},
	{Table: "orders", Name: "id"},
	{Table: "orders", Name: "total"},
}

func ref(table, name string) *ast.ColumnRef {
	return &ast.ColumnRef{Table: table, Name: name}
}

func TestResolveUnqualified(t *testing.T) {
	i, err := joined.Resolve(ref("", "total"))
	assert.NoError(t, err)
	assert.Equal(t, 3, i)
}

func TestResolveQualified(t *testing.T) {
	i, err := joined.Resolve(ref("orders", "id"))
	assert.NoError(t, err)
	assert.Equal(t, 2, i)

	i, err = joined.Resolve(ref("users", "id"))
	assert.NoError(t, err)
	assert.Equal(t, 0, i)
}

// The failure this whole type exists to prevent: two tables with an id column,
// and a bare reference that could mean either. First-match-wins would return
// plausible rows forever.
func TestResolveAmbiguousBareNameIsAnError(t *testing.T) {
	_, err := joined.Resolve(ref("", "id"))
	assert.ErrorContains(t, err, "ambiguous")
}

func TestResolveUnknownColumn(t *testing.T) {
	_, err := joined.Resolve(ref("", "nosuch"))
	assert.ErrorContains(t, err, "not found")
}

func TestResolveQualifierMustMatch(t *testing.T) {
	_, err := joined.Resolve(ref("orders", "name"))
	assert.ErrorContains(t, err, "not found")
}

func TestResolveIsCaseInsensitive(t *testing.T) {
	i, err := joined.Resolve(ref("ORDERS", "Total"))
	assert.NoError(t, err)
	assert.Equal(t, 3, i)
}

func TestExpandStar(t *testing.T) {
	idx, err := joined.Expand(&ast.Star{})
	assert.NoError(t, err)
	assert.DeepEqual(t, []int{0, 1, 2, 3}, idx)
}

func TestExpandQualifiedStar(t *testing.T) {
	idx, err := joined.Expand(&ast.Star{Table: "orders"})
	assert.NoError(t, err)
	assert.DeepEqual(t, []int{2, 3}, idx)
}

func TestExpandUnknownQualifier(t *testing.T) {
	_, err := joined.Expand(&ast.Star{Table: "nosuch"})
	assert.ErrorContains(t, err, "nosuch")
}

// An alias replaces the table name rather than joining it, which is the SQL
// rule: under FROM t AS x, x.a resolves and t.a does not.
func TestScanScopeAliasReplacesTheTableName(t *testing.T) {
	s := scanScope("t", "x", []string{"a", "b"})
	assert.DeepEqual(t, Scope{{Table: "x", Name: "a"}, {Table: "x", Name: "b"}}, s)

	_, err := s.Resolve(ref("t", "a"))
	assert.ErrorContains(t, err, "not found")

	i, err := s.Resolve(ref("x", "a"))
	assert.NoError(t, err)
	assert.Equal(t, 0, i)
}

func TestScanScopeWithoutAliasQualifiesByTableName(t *testing.T) {
	s := scanScope("t", "", []string{"a"})
	assert.DeepEqual(t, Scope{{Table: "t", Name: "a"}}, s)
}
