package storage

import (
	"github.com/vatsalpatel/sqlette/internal/btree"
	"github.com/vatsalpatel/sqlette/internal/record"
)

type Cursor interface {
	Next() bool
	Row() Row
	RowID() int64
	Err() error
	Close() error
}

type heapCursor struct {
	heap []Row
	idx  int
}

var _ Cursor = (*heapCursor)(nil)

func (c *heapCursor) Next() bool {
	c.idx++
	return c.idx < len(c.heap)
}
func (c *heapCursor) Row() Row     { return c.heap[c.idx] }
func (c *heapCursor) RowID() int64 { return int64(c.idx) }
func (c *heapCursor) Err() error   { return nil }
func (c *heapCursor) Close() error { return nil }

type btreeCursor struct {
	inner *btree.Cursor
	row   Row
	err   error
}

var _ Cursor = (*btreeCursor)(nil)

func (c *btreeCursor) Next() bool {
	if !c.inner.Next() {
		return false
	}
	c.inner.RowID()
	row, err := record.Decode(c.inner.Payload())
	if err != nil {
		c.err = err
		return false
	}
	c.row = row
	return true
}
func (c *btreeCursor) Row() Row     { return c.row }
func (c *btreeCursor) RowID() int64 { return c.inner.RowID() }
func (c *btreeCursor) Err() error   { return c.err }
func (c *btreeCursor) Close() error { return nil }
