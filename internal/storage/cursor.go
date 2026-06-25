package storage

type Cursor interface {
	Next() bool
	Row() Row
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

func (c *heapCursor) Row() Row { return c.heap[c.idx] }

func (c *heapCursor) Err() error { return nil }

func (c *heapCursor) Close() error { return nil }
