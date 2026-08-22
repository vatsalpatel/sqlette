package engine_test

import (
	"fmt"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
)

// Nothing queries an index until Stage F, so the only way to observe one from up
// here is to make it refuse something. A UNIQUE index is therefore the probe
// used throughout this file: if a duplicate is rejected, the index exists, is
// attached to the table, and holds an entry for the row being duplicated.

func execErr(t *testing.T, eng *engine.Engine, sql string) error {
	t.Helper()
	toks, err := lexer.Lex(sql)
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)
	_, err = eng.Exec(stmt)
	return err
}

func mustFail(t *testing.T, eng *engine.Engine, sql, why string) {
	t.Helper()
	if err := execErr(t, eng, sql); err == nil {
		t.Fatalf("%s: %q succeeded, want an error", why, sql)
	}
}

func rowCount(t *testing.T, eng *engine.Engine, table string) int {
	t.Helper()
	return len(mustExec(t, eng, "SELECT * FROM "+table).Rows)
}

func openWithPeople(t *testing.T, path string, names ...string) *engine.Engine {
	t.Helper()
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (name TEXT, age INT)")
	for i, n := range names {
		mustExec(t, eng, fmt.Sprintf("INSERT INTO t VALUES ('%s', %d)", n, 20+i))
	}
	return eng
}

func TestCreateIndexOnEmptyTable(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (name TEXT, age INT)")
	mustExec(t, eng, "CREATE INDEX idx_name ON t (name)")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_age ON t (age)")
	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 36)")

	assert.Equal(t, 1, rowCount(t, eng, "t"))
}

// The bulk build is the whole point of creating an index on a populated table:
// entries for rows that already existed. Duplicating a pre-existing row's value
// is the way to prove those entries are really there — a build that only started
// tracking new writes would let this through.
func TestCreateIndexBuildsEntriesForExistingRows(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada", "grace", "alan")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")

	mustFail(t, eng, "INSERT INTO t VALUES ('grace', 99)",
		"grace was inserted before the index was built")
	assert.Equal(t, 3, rowCount(t, eng, "t"))

	mustExec(t, eng, "INSERT INTO t VALUES ('linus', 99)")
	assert.Equal(t, 4, rowCount(t, eng, "t"))
}

// Building a UNIQUE index over data that already violates it has to fail, and
// has to leave nothing behind: no catalog entry, no half-populated tree. The
// follow-up statements are the real assertions — reusing the name proves the
// catalog is clean, and inserting the duplicate proves no index is enforcing.
func TestCreateUniqueIndexOverDuplicatesFailsAndLeavesNothing(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada", "ada")

	mustFail(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)",
		"the table already holds two rows named ada")

	mustExec(t, eng, "CREATE INDEX idx_name ON t (name)")
	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 50)")
	assert.Equal(t, 3, rowCount(t, eng, "t"))
}

func TestIndexSurvivesReopen(t *testing.T) {
	path := dbPath(t)

	eng := openWithPeople(t, path, "ada", "grace")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()

	mustFail(t, eng2, "INSERT INTO t VALUES ('ada', 99)", "the index should have been reattached on open")
	mustExec(t, eng2, "INSERT INTO t VALUES ('linus', 99)")

	// and the reattached index is maintained by writes made after the reopen
	mustFail(t, eng2, "INSERT INTO t VALUES ('linus', 100)", "linus was inserted after the reopen")
}

// reload() runs on open and after every rollback. If it rebuilds tables without
// reattaching their indexes, the engine keeps working and quietly stops
// maintaining every index in the database from that moment on. Nothing errors,
// so this is the test that has to catch it.
func TestIndexStillEnforcedAfterRollback(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")

	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "INSERT INTO t VALUES ('grace', 45)")
	mustExec(t, eng, "ROLLBACK")

	assert.Equal(t, 1, rowCount(t, eng, "t"))
	mustFail(t, eng, "INSERT INTO t VALUES ('ada', 99)", "the index must survive a rollback")

	// the rolled-back row is genuinely gone, so its name is free again
	mustExec(t, eng, "INSERT INTO t VALUES ('grace', 45)")
	mustFail(t, eng, "INSERT INTO t VALUES ('grace', 46)", "grace exists again after being reinserted")
}

// Same silent-reload hazard, reached the other way: the index itself is created
// inside the transaction being rolled back, so afterwards it must be gone.
func TestRollbackOfCreateIndexRemovesIt(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada")

	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")
	mustExec(t, eng, "ROLLBACK")

	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 99)")
	assert.Equal(t, 2, rowCount(t, eng, "t"))
}

func TestIndexMaintainedByUpdateAndDelete(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada", "grace")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")

	// freeing a value by updating the row that held it
	mustExec(t, eng, "UPDATE t SET name = 'Ada' WHERE name = 'ada'")
	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 60)")
	mustFail(t, eng, "INSERT INTO t VALUES ('Ada', 61)", "Ada is taken by the updated row")

	// freeing a value by deleting the row that held it
	mustExec(t, eng, "DELETE FROM t WHERE name = 'grace'")
	mustExec(t, eng, "INSERT INTO t VALUES ('grace', 62)")
	mustFail(t, eng, "INSERT INTO t VALUES ('grace', 63)", "grace is taken again")
}

func TestUniqueIndexAllowsRepeatedNulls(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (name TEXT, age INT)")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_name ON t (name)")

	for range 3 {
		mustExec(t, eng, "INSERT INTO t VALUES (NULL, 1)")
	}
	assert.Equal(t, 3, rowCount(t, eng, "t"))
}

func TestMultiColumnUniqueIndex(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (name TEXT, age INT)")
	mustExec(t, eng, "CREATE UNIQUE INDEX idx_pair ON t (name, age)")

	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 36)")
	mustExec(t, eng, "INSERT INTO t VALUES ('ada', 37)") // same name, different age
	mustFail(t, eng, "INSERT INTO t VALUES ('ada', 36)", "the whole pair is duplicated")
	assert.Equal(t, 2, rowCount(t, eng, "t"))
}

func TestCreateIndexErrors(t *testing.T) {
	eng := openWithPeople(t, dbPath(t), "ada")
	mustExec(t, eng, "CREATE INDEX idx_name ON t (name)")

	mustFail(t, eng, "CREATE INDEX idx_x ON nosuchtable (a)", "the table does not exist")
	mustFail(t, eng, "CREATE INDEX idx_y ON t (nosuchcolumn)", "the column does not exist")
	mustFail(t, eng, "CREATE INDEX idx_name ON t (age)", "the index name is already taken")

	// a failed CREATE INDEX leaves the table usable
	mustExec(t, eng, "INSERT INTO t VALUES ('grace', 45)")
	assert.Equal(t, 2, rowCount(t, eng, "t"))
}
