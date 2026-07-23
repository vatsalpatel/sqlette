package journal

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

// A tiny page keeps records small enough to truncate and corrupt by hand.
const testPageSize = 64

const recordSize = 4 + testPageSize + 4 // pageID | data | checksum

// page returns a testPageSize-byte page filled with b, so a page mix-up in
// Replay is visible rather than silent.
func page(b byte) []byte {
	p := make([]byte, testPageSize)
	for i := range p {
		p[i] = b
	}
	return p
}

func paths(t *testing.T) (dbPath, journalPath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db"), filepath.Join(dir, "test.db-journal")
}

// writeDB lays out pages into a fresh db file and leaves it open for Replay.
func writeDB(t *testing.T, path string, pages ...[]byte) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	assert.NoError(t, err)
	for i, p := range pages {
		_, err := f.WriteAt(p, int64(i)*testPageSize)
		assert.NoError(t, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func readPage(t *testing.T, f *os.File, id int) []byte {
	t.Helper()
	buf := make([]byte, testPageSize)
	_, err := f.ReadAt(buf, int64(id)*testPageSize)
	assert.NoError(t, err)
	return buf
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	assert.NoError(t, err)
	return info.Size()
}

func TestCreateWritesHeader(t *testing.T) {
	_, jPath := paths(t)

	j, err := Create(jPath, 7, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Sync())
	assert.NoError(t, j.Close())

	buf, err := os.ReadFile(jPath)
	assert.NoError(t, err)
	assert.Equal(t, headerSize, len(buf))
	assert.Equal(t, magic, string(buf[:8]))
	assert.Equal(t, uint32(7), binary.BigEndian.Uint32(buf[8:]))
	assert.Equal(t, uint32(testPageSize), binary.BigEndian.Uint32(buf[12:]))
}

func TestAppendRejectsWrongSizedPage(t *testing.T) {
	_, jPath := paths(t)

	j, err := Create(jPath, 1, testPageSize)
	assert.NoError(t, err)
	defer j.Close()

	assert.ErrorContains(t, j.Append(0, make([]byte, testPageSize-1)), "want")
	assert.ErrorContains(t, j.Append(0, make([]byte, testPageSize+1)), "want")
	assert.NoError(t, j.Append(0, page(0xAA)))
}

func TestDeleteRemovesJournal(t *testing.T) {
	_, jPath := paths(t)

	j, err := Create(jPath, 1, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Append(0, page(0xAA)))
	assert.NoError(t, j.Delete())

	_, err = os.Stat(jPath)
	assert.True(t, os.IsNotExist(err)) // the journal's absence is what "committed" means
}

// The core round-trip: pre-images go back to the pages they came from, and
// pages the transaction never touched are left alone.
func TestReplayRestoresPreImages(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA), page(0xBB), page(0xCC))

	j, err := Create(jPath, 3, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Append(0, page(0xAA)))
	assert.NoError(t, j.Append(2, page(0xCC)))
	assert.NoError(t, j.Sync())
	assert.NoError(t, j.Close())

	// the transaction modifies those two pages, then dies before commit
	_, err = db.WriteAt(page(0x11), 0)
	assert.NoError(t, err)
	_, err = db.WriteAt(page(0x22), 2*testPageSize)
	assert.NoError(t, err)

	dbPages, err := Replay(jPath, db)
	assert.NoError(t, err)
	assert.Equal(t, uint32(3), dbPages)

	assert.DeepEqual(t, page(0xAA), readPage(t, db, 0))
	assert.DeepEqual(t, page(0xBB), readPage(t, db, 1)) // never journaled, never touched
	assert.DeepEqual(t, page(0xCC), readPage(t, db, 2))
}

// A rolled-back transaction must give back the pages it allocated, so the file
// shrinks to the page count recorded in the header at BEGIN.
func TestReplayTruncatesToDBPages(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA), page(0xBB))

	j, err := Create(jPath, 2, testPageSize) // began with 2 pages
	assert.NoError(t, err)
	assert.NoError(t, j.Sync())
	assert.NoError(t, j.Close())

	_, err = db.WriteAt(page(0xDD), 2*testPageSize) // a page allocated mid-transaction
	assert.NoError(t, err)
	assert.Equal(t, int64(3*testPageSize), fileSize(t, dbPath))

	dbPages, err := Replay(jPath, db)
	assert.NoError(t, err)
	assert.Equal(t, uint32(2), dbPages)
	assert.Equal(t, int64(2*testPageSize), fileSize(t, dbPath))
}

// A journal torn mid-record (killed while appending) must replay its intact
// prefix and stop — not error, not apply garbage.
//
// The db state built here is artificial: in real operation page 1 could not
// hold post-mutation bytes while its pre-image record is torn, because a db
// page is never written until the journal is synced. That invariant is exactly
// what makes discarding the tail safe; mutating page 1 here is only how the
// test proves Replay actually stopped rather than silently applied it.
func TestReplayStopsAtTornRecord(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA), page(0xBB))

	j, err := Create(jPath, 2, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Append(0, page(0xAA)))
	assert.NoError(t, j.Append(1, page(0xBB)))
	assert.NoError(t, j.Close())

	// lop off half of the second record
	assert.NoError(t, os.Truncate(jPath, headerSize+recordSize+recordSize/2))

	_, err = db.WriteAt(page(0x11), 0)
	assert.NoError(t, err)
	_, err = db.WriteAt(page(0x22), testPageSize)
	assert.NoError(t, err)

	dbPages, err := Replay(jPath, db)
	assert.NoError(t, err) // a torn tail is expected, not a failure
	assert.Equal(t, uint32(2), dbPages)

	assert.DeepEqual(t, page(0xAA), readPage(t, db, 0)) // intact record applied
	assert.DeepEqual(t, page(0x22), readPage(t, db, 1)) // torn record discarded
}

// A record whose bytes are all present but corrupt is indistinguishable from a
// torn one, and gets the same treatment: stop there, keep the good prefix.
func TestReplayStopsAtBadChecksum(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA), page(0xBB))

	j, err := Create(jPath, 2, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Append(0, page(0xAA)))
	assert.NoError(t, j.Append(1, page(0xBB)))
	assert.NoError(t, j.Close())

	// flip the first payload byte of the second record, leaving its checksum stale
	f, err := os.OpenFile(jPath, os.O_RDWR, 0666)
	assert.NoError(t, err)
	_, err = f.WriteAt([]byte{0xFF}, headerSize+recordSize+4)
	assert.NoError(t, err)
	assert.NoError(t, f.Close())

	_, err = db.WriteAt(page(0x11), 0)
	assert.NoError(t, err)
	_, err = db.WriteAt(page(0x22), testPageSize)
	assert.NoError(t, err)

	dbPages, err := Replay(jPath, db)
	assert.NoError(t, err)
	assert.Equal(t, uint32(2), dbPages)

	assert.DeepEqual(t, page(0xAA), readPage(t, db, 0))
	assert.DeepEqual(t, page(0x22), readPage(t, db, 1)) // corrupt record discarded
}

// Recovery can itself be interrupted, so replaying an already-replayed journal
// must land on the same state. This is what lets Stage F treat crash recovery
// as nothing more than the rollback the dead process never ran.
func TestReplayIsIdempotent(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA), page(0xBB))

	j, err := Create(jPath, 2, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Append(0, page(0xAA)))
	assert.NoError(t, j.Sync())
	assert.NoError(t, j.Close())

	_, err = db.WriteAt(page(0x11), 0)
	assert.NoError(t, err)

	first, err := Replay(jPath, db)
	assert.NoError(t, err)
	second, err := Replay(jPath, db)
	assert.NoError(t, err)

	assert.Equal(t, first, second)
	assert.DeepEqual(t, page(0xAA), readPage(t, db, 0))
	assert.Equal(t, int64(2*testPageSize), fileSize(t, dbPath))
}

func TestReplayRejectsForeignFile(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA))

	assert.NoError(t, os.WriteFile(jPath, []byte("not a sqlette journal at all"), 0666))

	_, err := Replay(jPath, db)
	assert.ErrorContains(t, err, "magic")
}

// A header truncated mid-way is unreadable — distinct from a torn *record*,
// which is tolerated. There is nothing trustworthy to replay here.
func TestReplayRejectsShortHeader(t *testing.T) {
	dbPath, jPath := paths(t)
	db := writeDB(t, dbPath, page(0xAA))

	j, err := Create(jPath, 1, testPageSize)
	assert.NoError(t, err)
	assert.NoError(t, j.Close())
	assert.NoError(t, os.Truncate(jPath, headerSize-4))

	_, err = Replay(jPath, db)
	assert.ErrorContains(t, err, "header")
}
