package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func TestUpdateSetsColumn(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada')")

	res := mustExec(t, eng, "UPDATE t SET name = 'Ada'")
	assert.Equal(t, "1 rows updated", res.Message)

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("Ada")},
	}, sel.Rows)
}

// The SET target is resolved by column name, not by its position in the SET
// list: setting the third column must not touch the first.
func TestUpdateNonLeadingColumn(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT, age INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada', 36)")

	mustExec(t, eng, "UPDATE t SET age = 100")

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("ada"), values.NewInteger(100)},
	}, sel.Rows)
}

func TestUpdateMultipleColumns(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT, age INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada', 36)")

	mustExec(t, eng, "UPDATE t SET name = 'Ada', age = 100")

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("Ada"), values.NewInteger(100)},
	}, sel.Rows)
}

func TestUpdateExpressionRHS(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, n INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 5)")

	mustExec(t, eng, "UPDATE t SET n = n + 1")

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewInteger(6)},
	}, sel.Rows)
}

// Every RHS reads the pre-update row, so a two-assignment swap swaps rather than
// collapsing both columns to one value.
func TestUpdateSwap(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (a INT, b INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 2)")

	mustExec(t, eng, "UPDATE t SET a = b, b = a")

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(2), values.NewInteger(1)},
	}, sel.Rows)
}

func TestUpdateWithWhere(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, flag INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 0), (2, 0), (3, 0)")

	res := mustExec(t, eng, "UPDATE t SET flag = 1 WHERE id = 2")
	assert.Equal(t, "1 rows updated", res.Message)

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewInteger(0)},
		{values.NewInteger(2), values.NewInteger(1)},
		{values.NewInteger(3), values.NewInteger(0)},
	}, sel.Rows)
}

func TestUpdateMatchingNothing(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "UPDATE users SET age = 0 WHERE age > 100")
	assert.Equal(t, "0 rows updated", res.Message)

	sel := mustExec(t, eng, "SELECT name FROM users")
	assert.DeepEqual(t, []string{"ada", "alan", "grace", "bob"}, names(sel))
}

func TestUpdateUnknownColumnErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1)")

	_, err = tryExec(t, eng, "UPDATE t SET nope = 5")
	assert.True(t, err != nil)
}

func TestUpdatePersistsAcrossReopen(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada')")
	mustExec(t, eng, "UPDATE t SET name = 'Ada'")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()

	sel := mustExec(t, eng2, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewText("Ada")},
	}, sel.Rows)
}

func TestUpdateRolledBack(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT, val INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 10)")

	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "UPDATE t SET val = 99")
	inTxn := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewInteger(99)},
	}, inTxn.Rows)
	mustExec(t, eng, "ROLLBACK")

	sel := mustExec(t, eng, "SELECT * FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1), values.NewInteger(10)},
	}, sel.Rows)
}
