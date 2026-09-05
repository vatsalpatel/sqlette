package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// The two behaviours the scope refactor is allowed to change, and the only two.
// Everything else in the suite must return exactly what it returned before.
func aliasEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (a INT, b TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada'), (2, 'alan')")
	return eng
}

func TestSelectThroughATableAlias(t *testing.T) {
	eng := aliasEngine(t)

	res := mustExec(t, eng, "SELECT x.b FROM t AS x WHERE x.a = 2")
	assert.DeepEqual(t, []string{"b"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{{values.NewText("alan")}}, res.Rows)
}

// Once a table is aliased, its own name is out of scope. SQLite and Postgres
// both refuse this, and getting it wrong is how a join ends up resolving a
// column against the wrong side.
func TestAliasHidesTheTableName(t *testing.T) {
	eng := aliasEngine(t)

	_, err := tryExec(t, eng, "SELECT t.b FROM t AS x")
	assert.ErrorContains(t, err, "not found")

	_, err = tryExec(t, eng, "SELECT b FROM t AS x WHERE t.a = 1")
	assert.ErrorContains(t, err, "not found")
}

func TestQualifiedStarUnderAnAlias(t *testing.T) {
	eng := aliasEngine(t)

	res := mustExec(t, eng, "SELECT x.* FROM t AS x WHERE x.a = 1")
	assert.DeepEqual(t, []string{"a", "b"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("ada")},
	}, res.Rows)
}

// An unaliased table is still addressable by its own name, and a bare column
// still resolves whether or not an alias is in play.
func TestUnaliasedQualifierStillResolves(t *testing.T) {
	eng := aliasEngine(t)

	res := mustExec(t, eng, "SELECT t.b FROM t WHERE t.a = 1")
	assert.DeepEqual(t, [][]values.Value{{values.NewText("ada")}}, res.Rows)

	res = mustExec(t, eng, "SELECT b FROM t AS x WHERE a = 1")
	assert.DeepEqual(t, [][]values.Value{{values.NewText("ada")}}, res.Rows)
}
