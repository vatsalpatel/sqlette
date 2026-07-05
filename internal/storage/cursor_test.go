package storage_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func TestCursorExhaustionIsStable(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "t")
	insert(t, tbl, row(values.NewInteger(1)))

	cur := tbl.Scan()
	defer cur.Close()

	assert.True(t, cur.Next())
	assert.Equal(t, int64(1), cur.Row()[0].Int)
	assert.False(t, cur.Next())
	assert.False(t, cur.Next())
}

func TestCursorCloseReturnsNil(t *testing.T) {
	s := newStore(t)
	tbl := createTable(t, s, "t")

	cur := tbl.Scan()
	assert.NoError(t, cur.Close())
}
