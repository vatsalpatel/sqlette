package btree

import (
	"encoding/binary"
	"errors"

	"github.com/vatsalpatel/sqlette/internal/pager"
)

type node struct {
	page *pager.Page
}

type Tree struct {
	pager *pager.Pager
	root  pager.PageID
}

func Create(p *pager.Pager) (*Tree, error) {
	t := &Tree{pager: p}
	root, err := t.newLeaf()
	if err != nil {
		return nil, err
	}
	t.root = root.page.ID
	return t, nil
}

func Open(p *pager.Pager, root pager.PageID) *Tree {
	return &Tree{pager: p, root: root}
}

func (t *Tree) Root() pager.PageID {
	return t.root
}

func (t *Tree) load(id pager.PageID) (node, error) {
	p, err := t.pager.Get(id)
	return node{p}, err
}

func (t *Tree) newLeaf() (node, error) {
	p, err := t.pager.Allocate()
	if err != nil {
		return node{}, err
	}
	p.MarkDirty()
	return initLeaf(p), nil
}

func (t *Tree) newInterior(leftmost pager.PageID) (node, error) {
	p, err := t.pager.Allocate()
	if err != nil {
		return node{}, err
	}
	p.MarkDirty()
	return initInterior(p, leftmost), nil
}

func (t *Tree) search(rowid int64) (leaf node, slot int, found bool, err error) {
	n, err := t.load(t.root)
	if err != nil {
		return node{}, 0, false, err
	}
	for !n.isLeaf() {
		n, err = t.load(n.childPage(rowid))
		if err != nil {
			return node{}, 0, false, err
		}
	}
	slot, found = n.search(rowid)
	return n, slot, found, nil
}

func (t *Tree) Get(rowid int64) (payload []byte, found bool, err error) {
	leaf, slot, found, err := t.search(rowid)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return leaf.payload(slot), true, nil
}

func (t *Tree) Insert(rowid int64, payload []byte) error {
	if 8+binary.MaxVarintLen64+2+len(payload) > pager.PageSize-headerSize {
		return errors.New("btree: record too large for one page (overflow is M7)")
	}
	split, sep, right, err := t.insert(t.root, rowid, payload)
	if err != nil {
		return err
	}
	if split {
		root, err := t.newInterior(t.root)
		if err != nil {
			return err
		}
		root.insertInterior(sep, right)
		t.root = root.page.ID
	}
	return nil
}

func (t *Tree) insert(id pager.PageID, rowid int64, payload []byte) (split bool, sep int64, right pager.PageID, err error) {
	n, err := t.load(id)
	if err != nil {
		return false, 0, 0, err
	}
	if n.isLeaf() {
		if n.insertLeaf(rowid, payload) {
			return false, 0, 0, nil
		}
		sep, right, err = t.splitLeaf(n, rowid, payload)
		if err != nil {
			return false, 0, 0, err
		}
		return true, sep, right, nil
	}

	childSplit, sep, right, err := t.insert(n.childPage(rowid), rowid, payload)
	if err != nil || !childSplit {
		return false, 0, 0, err
	}
	if n.insertInterior(sep, right) {
		return false, 0, 0, nil
	}
	sep, right, err = t.splitInterior(n, sep, right)
	return true, sep, right, err
}

func (t *Tree) splitLeaf(full node, rowid int64, payload []byte) (int64, pager.PageID, error) {
	cells := make([]leafCell, 0, full.numCells()+1)
	inserted := false
	for i := 0; i < full.numCells(); i++ {
		k := full.key(i)
		if !inserted && rowid < k {
			cells = append(cells, leafCell{rowid, payload})
			inserted = true
		}
		cells = append(cells, leafCell{k, append([]byte(nil), full.payload(i)...)})
	}
	if !inserted {
		cells = append(cells, leafCell{rowid, payload})
	}

	mid := len(cells) / 2
	sep := cells[mid].key
	oldSibling := full.rightSibling()

	right, err := t.newLeaf()
	if err != nil {
		return 0, 0, err
	}

	initLeaf(full.page)
	for _, c := range cells[:mid] {
		full.insertLeaf(c.key, c.val)
	}
	for _, c := range cells[mid:] {
		right.insertLeaf(c.key, c.val)
	}

	right.setRightSibling(oldSibling)
	full.setRightSibling(right.page.ID)

	return sep, right.page.ID, nil
}

func (t *Tree) splitInterior(full node, rowid int64, child pager.PageID) (int64, pager.PageID, error) {
	cells := make([]interiorCell, 0, full.numCells()+1)
	inserted := false
	for i := 0; i < full.numCells(); i++ {
		k := full.key(i)
		if !inserted && rowid < k {
			cells = append(cells, interiorCell{rowid, child})
			inserted = true
		}
		cells = append(cells, interiorCell{k, full.child(i)})
	}
	if !inserted {
		cells = append(cells, interiorCell{rowid, child})
	}

	mid := len(cells) / 2
	median := cells[mid]
	leftmost := full.leftmostChild()

	right, err := t.newInterior(median.child)
	if err != nil {
		return 0, 0, err
	}
	for _, c := range cells[mid+1:] {
		right.insertInterior(c.sep, c.child)
	}

	initInterior(full.page, leftmost)
	for _, c := range cells[:mid] {
		full.insertInterior(c.sep, c.child)
	}

	return median.sep, right.page.ID, nil
}

type leafCell struct {
	key int64
	val []byte
}

type interiorCell struct {
	sep   int64
	child pager.PageID
}

func (t *Tree) Cursor() *Cursor {
	c := &Cursor{tree: t}
	n, err := t.load(t.root)
	if err != nil {
		c.err = err
		return c
	}
	for !n.isLeaf() {
		n, err = t.load(n.leftmostChild())
		if err != nil {
			c.err = err
			return c
		}
	}
	c.leaf = n
	c.slot = -1
	return c
}

type Cursor struct {
	tree *Tree
	leaf node
	slot int
	err  error
}

func (c *Cursor) Next() bool {
	if c.err != nil {
		return false
	}
	c.slot++
	if c.slot >= c.leaf.numCells() {
		sib := c.leaf.rightSibling()
		if sib == 0 {
			return false
		}
		c.leaf, c.err = c.tree.load(sib)
		if c.err != nil {
			return false
		}
		c.slot = 0
	}
	return c.slot < c.leaf.numCells()
}
func (c *Cursor) RowID() int64 {
	if c.err != nil {
		return 0
	}
	return c.leaf.key(c.slot)
}
func (c *Cursor) Payload() []byte {
	if c.err != nil {
		return nil
	}
	return c.leaf.payload(c.slot)
}
func (c *Cursor) Err() error   { return c.err }
func (c *Cursor) Close() error { return nil }
