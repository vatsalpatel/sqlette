package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func TestDeleteWithWhere(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "DELETE FROM users WHERE id = 2")
	assert.Equal(t, "1 rows deleted", res.Message)

	sel := mustExec(t, eng, "SELECT name FROM users")
	assert.DeepEqual(t, []string{"ada", "grace", "bob"}, names(sel))
}

func TestDeleteAllRows(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "DELETE FROM users")
	assert.Equal(t, "4 rows deleted", res.Message)

	sel := mustExec(t, eng, "SELECT * FROM users")
	assert.Equal(t, 0, len(sel.Rows))
}

// A WHERE that matches nothing deletes nothing and leaves the table untouched.
func TestDeleteMatchingNothing(t *testing.T) {
	eng := usersEngine(t)

	res := mustExec(t, eng, "DELETE FROM users WHERE age > 100")
	assert.Equal(t, "0 rows deleted", res.Message)

	sel := mustExec(t, eng, "SELECT name FROM users")
	assert.DeepEqual(t, []string{"ada", "alan", "grace", "bob"}, names(sel))
}

// After a delete the tree stays healthy: further inserts and scans work, and the
// survivors keep their order.
func TestDeleteThenInsert(t *testing.T) {
	eng := usersEngine(t)

	mustExec(t, eng, "DELETE FROM users WHERE age IS NULL") // drops bob (id 4)
	mustExec(t, eng, "INSERT INTO users VALUES (5, 'lynn', 33)")

	sel := mustExec(t, eng, "SELECT name FROM users")
	assert.DeepEqual(t, []string{"ada", "alan", "grace", "lynn"}, names(sel))
}

func TestDeletePersistsAcrossReopen(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada'), (2, 'alan'), (3, 'grace')")
	mustExec(t, eng, "DELETE FROM t WHERE id = 2")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()

	res := mustExec(t, eng2, "SELECT id FROM t")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(1)},
		{values.NewInteger(3)},
	}, res.Rows)
}

// A DELETE inside an explicit transaction is visible to the transaction's own
// reads, and ROLLBACK restores every row — pages via the pager, catalog/rowid
// state via reload().
func TestDeleteRolledBack(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1), (2), (3)")

	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "DELETE FROM t WHERE id = 2")
	inTxn := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 2, len(inTxn.Rows))
	mustExec(t, eng, "ROLLBACK")

	res := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 3, len(res.Rows))
}

func TestDeleteCommitPersists(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1), (2), (3)")
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "DELETE FROM t WHERE id = 2")
	mustExec(t, eng, "COMMIT")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	res := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 2, len(res.Rows))
}

func TestDeleteUnknownTableErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()

	_, err = tryExec(t, eng, "DELETE FROM nope WHERE id = 1")
	assert.True(t, err != nil)
}
