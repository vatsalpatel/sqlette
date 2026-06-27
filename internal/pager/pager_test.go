package pager_test

import (
	"path/filepath"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/pager"
)

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
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
	a.MarkDirty()

	b, err := p.Allocate()
	assert.NoError(t, err)
	mark(b, 0xBB)
	b.MarkDirty()

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

func TestUnmarkedMutationDoesNotPersist(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)

	a, err := p.Allocate()
	assert.NoError(t, err)
	id := a.ID
	mark(a, 0xCC)
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	defer p2.Close()

	g, err := p2.Get(id)
	assert.NoError(t, err)
	assert.False(t, marked(g, 0xCC))
}

func TestAllocateAfterReopenDoesNotClobber(t *testing.T) {
	path := tempDB(t)

	p, err := pager.Open(path)
	assert.NoError(t, err)
	a, err := p.Allocate()
	assert.NoError(t, err)
	mark(a, 0xAA)
	a.MarkDirty()
	b, err := p.Allocate()
	assert.NoError(t, err)
	mark(b, 0xBB)
	b.MarkDirty()
	idA, idB := a.ID, b.ID
	assert.NoError(t, p.Close())

	p2, err := pager.Open(path)
	assert.NoError(t, err)
	c, err := p2.Allocate()
	assert.NoError(t, err)
	mark(c, 0xCC)
	c.MarkDirty()
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
