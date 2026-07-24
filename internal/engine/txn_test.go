package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// dbPath and mustExec live in engine_test.go (same package).

// tryExec runs a statement and returns the result and error without asserting,
// for the cases that are supposed to fail.
func tryExec(t *testing.T, eng *engine.Engine, sql string) (*engine.Result, error) {
	t.Helper()
	toks, err := lexer.Lex(sql)
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)
	return eng.Exec(stmt)
}

// Each bare statement is its own transaction, so writes are durable without an
// explicit COMMIT.
func TestAutocommitPersistsAcrossReopen(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT, name TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada')")
	mustExec(t, eng, "INSERT INTO t VALUES (2, 'alan')")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	res := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 2, len(res.Rows))
}

// An explicit transaction commits several statements atomically, and the result
// survives a reopen.
func TestExplicitCommitPersistsAcrossReopen(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "INSERT INTO t VALUES (1)")
	mustExec(t, eng, "INSERT INTO t VALUES (2)")
	mustExec(t, eng, "COMMIT")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	res := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 2, len(res.Rows))
}

// ROLLBACK undoes the transaction's inserts but leaves the table (created and
// committed before BEGIN) and its committed rows intact.
func TestRollbackUndoesInserts(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1)") // committed via autocommit
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "INSERT INTO t VALUES (2)")
	mustExec(t, eng, "INSERT INTO t VALUES (3)")
	mustExec(t, eng, "ROLLBACK")

	res := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 1, len(res.Rows))
	assert.DeepEqual(t, []values.Value{values.NewInteger(1)}, res.Rows[0])
}

// The sneaky bug: the pager rolls back pages, but the catalog lives in memory.
// A rolled-back CREATE TABLE must vanish from the catalog too — proven by the
// table being re-creatable, which fails "already exists" if reload() is skipped.
func TestRollbackUndoesCreateTable(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "ROLLBACK")

	_, err = tryExec(t, eng, "SELECT * FROM t")
	assert.True(t, err != nil) // gone from the catalog

	_, err = tryExec(t, eng, "CREATE TABLE t (id INT)")
	assert.NoError(t, err) // fully removed → recreatable
}

// Inside a transaction, reads see the transaction's own uncommitted writes;
// after ROLLBACK they are gone.
func TestUncommittedWritesVisibleThenRolledBack(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "INSERT INTO t VALUES (1)")

	res := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 1, len(res.Rows)) // sees its own write

	mustExec(t, eng, "ROLLBACK")
	res = mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 0, len(res.Rows))
}

// A rolled-back transaction leaves nothing on disk either.
func TestRolledBackTransactionNotPersisted(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "INSERT INTO t VALUES (1)")
	mustExec(t, eng, "ROLLBACK")
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	res := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 0, len(res.Rows))
}

// A statement that fails partway through in autocommit must leave nothing: the
// first row inserts, the second errors on arity, and the whole statement rolls
// back.
func TestFailedAutocommitStatementLeavesNothing(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "CREATE TABLE t (id INT)")

	_, err = tryExec(t, eng, "INSERT INTO t VALUES (1), (2, 3)")
	assert.True(t, err != nil)

	res := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 0, len(res.Rows))
}

func TestNestedBeginErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()
	mustExec(t, eng, "BEGIN")
	_, err = tryExec(t, eng, "BEGIN")
	assert.ErrorContains(t, err, "transaction")
}

func TestCommitOrRollbackWithoutTransactionErrors(t *testing.T) {
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()

	_, err = tryExec(t, eng, "COMMIT")
	assert.ErrorContains(t, err, "no transaction")

	_, err = tryExec(t, eng, "ROLLBACK")
	assert.ErrorContains(t, err, "no transaction")
}
