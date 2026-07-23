package pager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	assert.NoError(t, err)
	return info.Size()
}

func mark(p *pager.Page, b byte) {
	p.Data[0] = b
	p.Data[pager.PageSize-1] = b
}

func marked(p *pager.Page, b byte) bool {
	return p.Data[0] == b && p.Data[pager.PageSize-1] == b
}

func TestAllocateAssignsConsecutiveIDs(t *testing.T) {
	p, err := pager.Open(tempDB(t))
	assert.NoError(t, err)
	defer p.Close()

	a, err := p.Allocate()
	assert.NoError(t, err)
	b, err := p.Allocate()
	assert.NoError(t, err)
	c, err := p.Allocate()
	assert.NoError(t, err)

	assert.Equal(t, a.ID+1, b.ID)
	assert.Equal(t, b.ID+1, c.ID)
}

func TestGetReturnsSameCachedPage(t *testing.T) {
	p, err := pager.Open(tempDB(t))
	assert.NoError(t, err)
	defer p.Close()

	a, err := p.Allocate()
	assert.NoError(t, err)

	g, err := p.Get(a.ID)
	assert.NoError(t, err)
	assert.True(t, a == g)

	a.Data[0] = 0x42
	assert.Equal(t, byte(0x42), g.Data[0])
}

func TestRoundTripThroughReopen(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)

	a, err := p.Allocate()
	assert.NoError(t, err)
	mark(a, 0xAA)

	b, err := p.Allocate()
	assert.NoError(t, err)
	mark(b, 0xBB)

	idA, idB := a.ID, b.ID
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	ga, err := p2.Get(idA)
	assert.NoError(t, err)
	assert.True(t, marked(ga, 0xAA))

	gb, err := p2.Get(idB)
	assert.NoError(t, err)
	assert.True(t, marked(gb, 0xBB))
}

// A page read from disk and mutated without announcing it via Write must not
// survive a flush. Freshly allocated pages are born dirty and always persist, so
// this exercises the read-from-disk path specifically — the write-barrier
// contract that Stage E hangs the journal's pre-image capture on.
func TestUnmarkedMutationDoesNotPersist(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	a, err := p.Allocate()
	assert.NoError(t, err)
	id := a.ID
	mark(a, 0xAA) // allocated pages are dirty, so this persists
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	g, err := p2.Get(id) // clean page read from disk
	assert.NoError(t, err)
	mark(g, 0xCC) // mutate but never announce via Write
	assert.NoError(t, p2.Close())

	p3, err := pager.Open(path)
	assert.NoError(t, err)
	defer p3.Close()

	g3, err := p3.Get(id)
	assert.NoError(t, err)
	assert.True(t, marked(g3, 0xAA))  // the flushed value survives
	assert.False(t, marked(g3, 0xCC)) // the unannounced mutation does not
}

// Allocation is deferred: it touches memory only. The file grows at Flush,
// which is what lets a rolled-back transaction leave no trace on disk.
func TestAllocateDefersFileGrowth(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path) // reserves page 0 in memory, no write yet
	assert.NoError(t, err)
	defer p.Close()

	assert.Equal(t, int64(0), fileSize(t, path))

	_, err = p.Allocate()
	assert.NoError(t, err)
	_, err = p.Allocate()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), fileSize(t, path)) // still deferred

	assert.NoError(t, p.Flush())
	assert.Equal(t, int64(3*pager.PageSize), fileSize(t, path)) // page 0 + 2 allocated
}

func TestAllocateAfterReopenDoesNotClobber(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	a, err := p.Allocate()
	assert.NoError(t, err)
	mark(a, 0xAA)
	b, err := p.Allocate()
	assert.NoError(t, err)
	mark(b, 0xBB)
	idA, idB := a.ID, b.ID
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	c, err := p2.Allocate()
	assert.NoError(t, err)
	mark(c, 0xCC)
	assert.True(t, c.ID != idA && c.ID != idB)
	assert.NoError(t, p2.Close())

	p3, err := pager.Open(path)
	assert.NoError(t, err)
	defer p3.Close()

	ga, err := p3.Get(idA)
	assert.NoError(t, err)
	assert.True(t, marked(ga, 0xAA))
	gb, err := p3.Get(idB)
	assert.NoError(t, err)
	assert.True(t, marked(gb, 0xBB))
}
