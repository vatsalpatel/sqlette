package pager_test

import (
	"os"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/journal"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

// helpers tempDB, mark, marked, fileSize (pager_test.go) and journalPath,
// hasJournal, commitPage (txn_test.go) are shared across the package.

// These tests reconstruct the on-disk state a process leaves when it dies
// mid-commit: the db file has post-mutation (or newly allocated) pages, and a
// synced hot journal sits beside it holding the pre-images. Opening the pager
// must run recovery before serving any page.

type jrec struct {
	id   uint32
	data []byte
}

func markedPage(b byte) []byte {
	p := make([]byte, pager.PageSize)
	p[0] = b
	p[pager.PageSize-1] = b
	return p
}

func rawPage(t *testing.T, path string, id int) []byte {
	t.Helper()
	f, err := os.Open(path)
	assert.NoError(t, err)
	defer f.Close()
	buf := make([]byte, pager.PageSize)
	_, err = f.ReadAt(buf, int64(id)*int64(pager.PageSize))
	assert.NoError(t, err)
	return buf
}

func writeRawPage(t *testing.T, path string, id int, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0666)
	assert.NoError(t, err)
	defer f.Close()
	_, err = f.WriteAt(data, int64(id)*int64(pager.PageSize))
	assert.NoError(t, err)
}

func pageCount(t *testing.T, path string) uint32 {
	t.Helper()
	return uint32(fileSize(t, path) / int64(pager.PageSize))
}

// writeHotJournal builds the journal a crashed transaction would have left: a
// header recording the db's page count at BEGIN, then a synced pre-image per
// record. Synced, but never deleted — which is what makes it "hot".
func writeHotJournal(t *testing.T, dbPath string, dbPages uint32, records ...jrec) {
	t.Helper()
	j, err := journal.Create(journalPath(dbPath), dbPages, uint32(pager.PageSize))
	assert.NoError(t, err)
	for _, r := range records {
		assert.NoError(t, j.Append(r.id, r.data))
	}
	assert.NoError(t, j.Sync())
	assert.NoError(t, j.Close())
}

// The core: a hot journal is detected on open, its pre-images restored, and the
// journal removed — the rollback the dead process never got to run.
func TestOpenRecoversHotJournal(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	id := commitPage(t, p, 0xAA)
	assert.NoError(t, p.Close())

	original := rawPage(t, path, int(id)) // the true committed bytes

	// crash mid-commit: the db page was overwritten and a hot journal holding
	// its pre-image was left behind
	writeRawPage(t, path, int(id), markedPage(0xBB))
	writeHotJournal(t, path, pageCount(t, path), jrec{uint32(id), original})

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	assert.False(t, hasJournal(t, path)) // consumed and removed
	g, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g, 0xAA))  // pre-image restored
	assert.False(t, marked(g, 0xBB)) // the crashed write undone
}

// Recovery truncates the file back to the journal's dbPages, so Count must be
// derived AFTER recovery — not from the larger, half-committed file size. This
// is the ordering trap in Open.
func TestOpenRecoveryTruncatesAllocatedPages(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	id := commitPage(t, p, 0xAA)
	assert.NoError(t, p.Close())

	original := rawPage(t, path, int(id))
	pagesAtBegin := pageCount(t, path) // the count the crashed txn started from

	// crash: page `id` modified AND a new page allocated beyond dbPages, then
	// death before the journal was deleted
	writeRawPage(t, path, int(id), markedPage(0xBB))
	writeRawPage(t, path, int(pagesAtBegin), markedPage(0xCC)) // grows the file
	writeHotJournal(t, path, pagesAtBegin, jrec{uint32(id), original})

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	assert.Equal(t, pager.PageID(pagesAtBegin), p2.Count)
	assert.Equal(t, int64(pagesAtBegin)*int64(pager.PageSize), fileSize(t, path))
	g, err := p2.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g, 0xAA))
}

// Recovery is idempotent and self-clearing: once it has run, the journal is
// gone, so a second open finds nothing to do and behaves like any normal open.
func TestSecondOpenAfterRecoveryIsNoOp(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	id := commitPage(t, p, 0xAA)
	assert.NoError(t, p.Close())

	original := rawPage(t, path, int(id))
	writeRawPage(t, path, int(id), markedPage(0xBB))
	writeHotJournal(t, path, pageCount(t, path), jrec{uint32(id), original})

	p2, err := pager.Open(path) // recovers
	assert.NoError(t, err)
	assert.NoError(t, p2.Close())

	assert.False(t, hasJournal(t, path))

	p3, err := pager.Open(path) // nothing to recover
	assert.NoError(t, err)
	defer p3.Close()
	g, err := p3.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g, 0xAA))
}
