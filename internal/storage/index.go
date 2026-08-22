package storage

import (
	"bytes"
	"fmt"
	"math"

	"github.com/vatsalpatel/sqlette/internal/btree"
	"github.com/vatsalpatel/sqlette/internal/pager"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type Index struct {
	tree    *btree.Tree
	columns []int
	unique  bool
}

type Bound struct {
	Value     values.Value
	Inclusive bool
}

func newIndex(tree *btree.Tree, columns []int, unique bool) *Index {
	return &Index{tree: tree, columns: columns, unique: unique}
}

func createIndexTree(p *pager.Pager) (*btree.Tree, error) {
	return btree.CreateIndex(p, compareIndexKeys)
}

func openIndexTree(p *pager.Pager, root pager.PageID) *btree.Tree {
	return btree.OpenIndex(p, root, compareIndexKeys)
}

func (ix *Index) Root() pager.PageID { return ix.tree.Root() }

func indexKey(cols []values.Value, rowid int64) []byte {
	k := make([]values.Value, len(cols), len(cols)+1)
	copy(k, cols)
	return record.Encode(append(k, values.NewInteger(rowid)))
}

func compareIndexKeys(a, b []byte) int {
	x, err := record.Decode(a)
	if err != nil {
		panic(fmt.Errorf("storage: corrupt index key: %v", err.Error()))
	}
	y, err := record.Decode(b)
	if err != nil {
		panic(fmt.Errorf("storage: corrupt index key: %v", err.Error()))
	}
	return compareTuples(x, y)
}

func compareTuples(x, y []values.Value) int {
	for i := range min(len(x), len(y)) {
		if c := values.Compare(x[i], y[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(x) < len(y):
		return -1
	case len(x) > len(y):
		return 1
	default:
		return 0
	}
}

func (ix *Index) keyCols(row Row) []values.Value {
	cols := make([]values.Value, len(ix.columns))
	for i, col := range ix.columns {
		cols[i] = row[col]
	}
	return cols
}

func (ix *Index) Insert(row Row, rowid int64) error {
	return ix.tree.InsertKey(indexKey(ix.keyCols(row), rowid))
}

func (ix *Index) Delete(row Row, rowid int64) (bool, error) {
	return ix.tree.DeleteKey(indexKey(ix.keyCols(row), rowid))
}

func (ix *Index) SeekPrefix(cols []values.Value) *btree.Cursor {
	low := make([]values.Value, len(ix.columns))
	for i := range low {
		if i < len(cols) {
			low[i] = cols[i]
			continue
		}
		low[i] = values.NewNull()
	}
	return ix.tree.Seek(indexKey(low, math.MinInt64))
}

func (ix *Index) HasPrefix(cols []values.Value) (bool, error) {
	c := ix.SeekPrefix(cols)
	defer c.Close()

	if !c.Next() {
		return false, c.Err()
	}
	entry, err := record.Decode(c.Key())
	if err != nil {
		return false, err
	}
	return matchesPrefix(cols, entry), nil
}

func matchesPrefix(cols []values.Value, entry []values.Value) bool {
	if len(entry) < len(cols) {
		return false
	}
	for i := range cols {
		if values.Compare(entry[i], cols[i]) != 0 {
			return false
		}
	}
	return true
}

func (ix *Index) sameKey(a, b Row) bool {
	return bytes.Equal(record.Encode(ix.keyCols(a)), record.Encode(ix.keyCols(b)))
}

func (ix *Index) checkUnique(row Row, self int64) error {
	if !ix.unique {
		return nil
	}
	cols := ix.keyCols(row)
	for _, v := range cols {
		if v.Type == values.Null {
			return nil
		}
	}

	c := ix.SeekPrefix(cols)
	defer c.Close()
	for c.Next() {
		entry, err := record.Decode(c.Key())
		if err != nil {
			return err
		}
		if !matchesPrefix(cols, entry) {
			break // walked past the prefix, so nothing else can match
		}
		if entry[len(entry)-1].Int != self {
			return fmt.Errorf("storage: UNIQUE constraint failed")
		}
	}
	return c.Err()
}

type indexCursor struct {
	table     *Table
	ix        *Index
	inner     *btree.Cursor
	low, high *Bound
	row       Row
	rowid     int64
	err       error
}

var _ Cursor = (*indexCursor)(nil)

func (c *indexCursor) Next() bool {
	if c.err != nil {
		return false
	}
	for c.inner.Next() {
		entry, err := record.Decode(c.inner.Key())
		if err != nil {
			c.err = err
			return false
		}
		if len(entry) < 2 {
			c.err = fmt.Errorf("storage: index entry has %d fields, want at least 2", len(entry))
			return false
		}
		load := entry[0]

		if c.low != nil && !c.low.Inclusive && values.Compare(load, c.low.Value) <= 0 {
			continue
		}
		if c.high != nil {
			cmp := values.Compare(load, c.high.Value)
			if cmp > 0 || (cmp == 0 && !c.high.Inclusive) {
				return false
			}
		}

		rowid := entry[len(entry)-1].Int
		row, found, err := c.table.Get(rowid)
		if err != nil {
			c.err = err
			return false
		}
		if !found {
			c.err = fmt.Errorf("storage: index points at missing row %d", rowid)
			return false
		}
		c.row, c.rowid = row, rowid
		return true
	}
	c.err = c.inner.Err()
	return false
}

func (c *indexCursor) Row() Row     { return c.row }
func (c *indexCursor) RowID() int64 { return c.rowid }
func (c *indexCursor) Err() error   { return c.err }
func (c *indexCursor) Close() error { return c.inner.Close() }

func (t *Table) IndexScan(ix *Index, low, high *Bound) Cursor {
	c := &indexCursor{ix: ix, table: t, low: low, high: high}
	if low != nil {
		c.inner = ix.SeekPrefix([]values.Value{low.Value})
	} else {
		c.inner = ix.tree.Cursor()
	}
	return c
}
