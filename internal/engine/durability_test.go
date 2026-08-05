package engine_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

// End-to-end reclaim proof: every iteration inserts a fat row then deletes it,
// so the table is empty at each commit. With the leaf repacked on delete the
// file never grows past a handful of pages; without it the btree would split
// hundreds of times and the file would balloon into the megabytes.
func TestDeleteInsertLoopKeepsFileSmall(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT, blob TEXT)")

	blob := strings.Repeat("x", 1500)
	for i := range 500 {
		mustExec(t, eng, fmt.Sprintf("INSERT INTO t VALUES (%d, '%s')", i, blob))
		mustExec(t, eng, "DELETE FROM t")
	}
	assert.NoError(t, eng.Close())

	info, err := os.Stat(path)
	assert.NoError(t, err)
	if max := int64(16 * pager.PageSize); info.Size() > max {
		t.Fatalf("file grew to %d bytes, want <= %d (space not reclaimed?)", info.Size(), max)
	}
}

// A bulk DELETE inside a transaction spans many leaves; ROLLBACK must restore
// every row — pages via the pager, catalog/rowid via reload() — and leave
// nothing on disk, verified by a reopen.
func TestRollbackRestoresBulkDelete(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	for i := range 100 {
		mustExec(t, eng, fmt.Sprintf("INSERT INTO t VALUES (%d)", i))
	}

	mustExec(t, eng, "BEGIN")
	mustExec(t, eng, "DELETE FROM t")
	empty := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 0, len(empty.Rows))
	mustExec(t, eng, "ROLLBACK")

	back := mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 100, len(back.Rows))
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	reopened := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 100, len(reopened.Rows))
}

func TestCommitBulkDeletePersists(t *testing.T) {
	path := dbPath(t)
	eng, err := engine.Open(path)
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (id INT)")
	for i := range 100 {
		mustExec(t, eng, fmt.Sprintf("INSERT INTO t VALUES (%d)", i))
	}

	res := mustExec(t, eng, "DELETE FROM t WHERE id < 50")
	assert.Equal(t, "50 rows deleted", res.Message)
	assert.NoError(t, eng.Close())

	eng2, err := engine.Open(path)
	assert.NoError(t, err)
	defer eng2.Close()
	remaining := mustExec(t, eng2, "SELECT * FROM t")
	assert.Equal(t, 50, len(remaining.Rows))
}
