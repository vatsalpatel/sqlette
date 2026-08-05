package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func dbPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func mustExec(t *testing.T, eng *engine.Engine, sql string) *engine.Result {
	t.Helper()
	toks, err := lexer.Lex(sql)
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)
	res, err := eng.Exec(stmt)
	assert.NoError(t, err)
	return res
}

func TestCreateInsertSelectStar(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE users (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO users VALUES (1, 'ada')")
	mustExec(t, eng, "INSERT INTO users VALUES (2, 'alan')")

	res := mustExec(t, eng, "SELECT * FROM users")

	assert.DeepEqual(t, []string{"id", "name"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("ada")},
		{values.NewInteger(2), values.NewText("alan")},
	}, res.Rows)
}

func TestSelectColumnSubset(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE users (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO users VALUES (1, 'ada')")

	res := mustExec(t, eng, "SELECT name FROM users")

	assert.DeepEqual(t, []string{"name"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{
		{values.NewText("ada")},
	}, res.Rows)
}

func TestInsertMultipleRows(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (a INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1), (2), (3)")

	res := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 3, len(res.Rows))
}

func TestSelectUnknownTableErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	toks, err := lexer.Lex("SELECT * FROM nope")
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)

	_, err = eng.Exec(stmt)
	assert.True(t, err != nil)
}

func TestSelectWhere(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE users (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO users VALUES (1, 'ada')")
	mustExec(t, eng, "INSERT INTO users VALUES (2, 'alan')")

	res := mustExec(t, eng, "SELECT * FROM users WHERE id = 2")

	assert.DeepEqual(t, []string{"id", "name"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(2), values.NewText("alan")},
	}, res.Rows)

	res = mustExec(t, eng, "SELECT * FROM users WHERE id != 2")
	assert.DeepEqual(t, []string{"id", "name"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("ada")},
	}, res.Rows)
}

func TestExplainSelect(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE users (id INT, name TEXT)")

	res := mustExec(t, eng, "EXPLAIN SELECT * FROM users WHERE id > 1")
	assert.Equal(t, "(project *)\n  (filter (> id 1))\n    (seqscan users)", res.Message)
}

func TestExplainDelete(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "EXPLAIN DELETE FROM users WHERE id = 2")
	assert.Equal(t, "(delete users)\n  (filter (= id 2))\n    (seqscan users)", res.Message)

	res = mustExec(t, eng, "EXPLAIN DELETE FROM users")
	assert.Equal(t, "(delete users)\n  (seqscan users)", res.Message)

	// EXPLAIN builds the plan but must not run it.
	sel := mustExec(t, eng, "SELECT * FROM users")
	assert.Equal(t, 4, len(sel.Rows))
}

func TestExplainUpdate(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "EXPLAIN UPDATE users SET age = 0 WHERE id = 1")
	assert.Equal(t, "(update users)\n  (filter (= id 1))\n    (seqscan users)", res.Message)

	res = mustExec(t, eng, "EXPLAIN UPDATE users SET age = 0")
	assert.Equal(t, "(update users)\n  (seqscan users)", res.Message)

	sel := mustExec(t, eng, "SELECT name FROM users")
	assert.DeepEqual(t, []string{"ada", "alan", "grace", "bob"}, names(sel))
}

func TestExplainUnknownTableErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()

	_, err = tryExec(t, eng, "EXPLAIN SELECT * FROM nope")
	assert.True(t, err != nil)
}
