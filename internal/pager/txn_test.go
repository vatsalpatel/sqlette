package pager_test

import (
	"os"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

// helpers tempDB, mark, marked, fileSize live in pager_test.go (same package).

func journalPath(dbPath string) string { return dbPath + "-journal" }

func hasJournal(t *testing.T, dbPath string) bool {
	t.Helper()
	_, err := os.Stat(journalPath(dbPath))
	return err == nil
}

// commitPage is a small fixture: allocate one page, stamp it, commit. It leaves
// a clean on-disk baseline the transaction tests can then modify or roll back.
func commitPage(t *testing.T, p *pager.Pager, b byte) pager.PageID {
	t.Helper()
	assert.NoError(t, p.Begin())
	pg, err := p.Allocate()
	assert.NoError(t, err)
	mark(pg, b)
	assert.NoError(t, p.Commit())
	return pg.ID
}

// The journal is created lazily on the first write of an existing page, and
// deleted at commit — its absence is precisely what "committed" means.
func TestJournalLifecycleAcrossWriteTransaction(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)
	defer p.Close()

	id := commitPage(t, p, 0xAA)

	assert.NoError(t, p.Begin())
	assert.False(t, hasJournal(t, path)) // no write yet

	g, err := p.Get(id)
	assert.NoError(t, err)
	assert.NoError(t, p.Write(g)) // first write of an existing page
	assert.True(t, hasJournal(t, path))
	mark(g, 0xBB)

	assert.NoError(t, p.Commit())
	assert.False(t, hasJournal(t, path)) // deleted at commit
}

// A transaction that only allocates fresh pages journals nothing, but its dirty
// pages must still be flushed at commit. journal==nil is NOT "nothing to do".
func TestCommitPersistsFreshPagesWithoutJournal(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)

	assert.NoError(t, p.Begin())
	a, err := p.Allocate()
	assert.NoError(t, err)
	id := a.ID
	mark(a, 0xAA)
	assert.False(t, hasJournal(t, path)) // fresh page → never journaled

	assert.NoError(t, p.Commit())
	// Commit must have flushed despite journal==nil: page 0 (header) + page 1.
	assert.Equal(t, int64(2*pager.PageSize), fileSize(t, path))
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()
	g, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g, 0xAA))
}

// Committing a modification to an existing page persists the new content and
// survives a reopen; no journal is left behind.
func TestCommitPersistsModifiedPage(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)

	id := commitPage(t, p, 0xAA)

	assert.NoError(t, p.Begin())
	g, err := p.Get(id)
	assert.NoError(t, err)
	assert.NoError(t, p.Write(g))
	mark(g, 0xBB)
	assert.NoError(t, p.Commit())

	assert.False(t, hasJournal(t, path))
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()
	g2, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g2, 0xBB))
}

// Rollback restores a modified existing page to its pre-transaction content —
// both in-process (after the cache is dropped) and across a reopen.
func TestRollbackRestoresModifiedPage(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)

	id := commitPage(t, p, 0xAA)

	assert.NoError(t, p.Begin())
	g, err := p.Get(id)
	assert.NoError(t, err)
	assert.NoError(t, p.Write(g))
	mark(g, 0xBB)
	assert.NoError(t, p.Rollback())

	assert.False(t, hasJournal(t, path))

	g2, err := p.Get(id) // cache was dropped; this re-reads the restored page
	assert.NoError(t, err)
	assert.True(t, marked(g2, 0xAA))
	assert.False(t, marked(g2, 0xBB))

	assert.NoError(t, p.Close())
	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()
	g3, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g3, 0xAA))
}

// Writing a page twice in one transaction must capture its pre-image exactly
// once — the state at BEGIN, not one edit ago. Rollback restores 0xAA, never
// the intermediate 0xBB. This is the whole point of the already-dirty check.
func TestRollbackAfterMultipleWritesRestoresPreTxnState(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)
	defer p.Close()

	id := commitPage(t, p, 0xAA)

	assert.NoError(t, p.Begin())
	g, err := p.Get(id)
	assert.NoError(t, err)
	assert.NoError(t, p.Write(g)) // captures pre-image 0xAA
	mark(g, 0xBB)
	assert.NoError(t, p.Write(g)) // already dirty → must NOT recapture 0xBB
	mark(g, 0xCC)
	assert.NoError(t, p.Rollback())

	g2, err := p.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g2, 0xAA))
}

// A rolled-back transaction gives back every page it allocated: the page count
// and the file size return to their pre-transaction values.
func TestRollbackDiscardsAllocatedPages(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)

	commitPage(t, p, 0xAA)
	countBefore := p.Count
	assert.NoError(t, p.Close())
	sizeBefore := fileSize(t, path)

	p, err = pager.Open(path)
	assert.NoError(t, err)

	assert.NoError(t, p.Begin())
	for i := 0; i < 5; i++ {
		np, err := p.Allocate()
		assert.NoError(t, err)
		mark(np, byte(0xC0+i))
	}
	assert.NoError(t, p.Rollback())
	assert.Equal(t, countBefore, p.Count)

	assert.NoError(t, p.Close())
	assert.Equal(t, sizeBefore, fileSize(t, path))
}

// A read-only transaction touches no files: it must never create a journal.
func TestReadOnlyTransactionCreatesNoJournal(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)
	defer p.Close()

	id := commitPage(t, p, 0xAA)

	assert.NoError(t, p.Begin())
	_, err = p.Get(id)
	assert.NoError(t, err)
	assert.False(t, hasJournal(t, path)) // no Write → no journal
	assert.NoError(t, p.Commit())
	assert.False(t, hasJournal(t, path))
}

// Rolling back on a brand-new database that has never been flushed must not
// error: the file may be smaller than startCount*PageSize (pages 0 and 1 exist
// only in the cache), so a later Get would read past EOF unless Rollback
// truncates the file up to the restored page count.
func TestRollbackOnFreshDatabase(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path) // page 0 reserved in memory; file is 0 bytes
	assert.NoError(t, err)
	defer p.Close()

	assert.NoError(t, p.Begin())
	a, err := p.Allocate()
	assert.NoError(t, err)
	mark(a, 0xAA)
	assert.NoError(t, p.Rollback())

	assert.Equal(t, pager.PageID(1), p.Count)
	_, err = p.Get(0) // must not fail reading a never-written page
	assert.NoError(t, err)
}

// The pre-Stage-E path is unchanged: without an enclosing transaction, Write
// just marks dirty (no journal), and Close still persists.
func TestNoTransactionStillPersists(t *testing.T) {
	path := tempDB(t)
	p, err := pager.Open(path)
	assert.NoError(t, err)

	a, err := p.Allocate()
	assert.NoError(t, err)
	id := a.ID
	mark(a, 0xAA)
	assert.NoError(t, p.Write(a))
	assert.False(t, hasJournal(t, path)) // no Begin → Write never journals
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()
	g, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g, 0xAA))
}
