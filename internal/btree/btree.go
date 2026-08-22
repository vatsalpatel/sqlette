package btree

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/vatsalpatel/sqlette/internal/pager"
)

type node struct {
	page *pager.Page
	cmp  Compare
}

type Tree struct {
	pager *pager.Pager
	root  pager.PageID
	kind  treeKind
	cmp   Compare
}

func Create(p *pager.Pager) (*Tree, error) { return create(p, tableTree, compareRowid) }
func CreateIndex(p *pager.Pager, cmp Compare) (*Tree, error) {
	return create(p, indexTree, cmp)
}

func create(p *pager.Pager, k treeKind, cmp Compare) (*Tree, error) {
	t := &Tree{pager: p, kind: k, cmp: cmp}
	root, err := t.newLeaf()
	if err != nil {
		return nil, err
	}
	t.root = root.page.ID
	return t, nil
}

func Open(p *pager.Pager, root pager.PageID) *Tree {
	return &Tree{pager: p, root: root, kind: tableTree, cmp: compareRowid}
}
func OpenIndex(p *pager.Pager, root pager.PageID, cmp Compare) *Tree {
	return &Tree{pager: p, root: root, kind: indexTree, cmp: cmp}
}

func (t *Tree) Root() pager.PageID {
	return t.root
}

func (t *Tree) load(id pager.PageID) (node, error) {
	p, err := t.pager.Get(id)
	return node{p, t.cmp}, err
}

func (t *Tree) newLeaf() (node, error) {
	p, err := t.pager.Allocate()
	if err != nil {
		return node{}, err
	}
	return initLeaf(p, t.kind, t.cmp), nil
}

func (t *Tree) newInterior(leftmost pager.PageID) (node, error) {
	p, err := t.pager.Allocate()
	if err != nil {
		return node{}, err
	}
	return initInterior(p, leftmost, t.kind, t.cmp), nil
}

func (t *Tree) search(key []byte) (leaf node, slot int, found bool, err error) {
	n, err := t.load(t.root)
	if err != nil {
		return node{}, 0, false, err
	}
	for !n.isLeaf() {
		n, err = t.load(n.childPage(key))
		if err != nil {
			return node{}, 0, false, err
		}
	}
	slot, found = n.search(key)
	return n, slot, found, nil
}

func (t *Tree) Get(rowid int64) (payload []byte, found bool, err error) {
	var key [rowidKeySize]byte
	putRowid(key[:], rowid)
	return t.get(key[:])
}

func (t *Tree) get(key []byte) (payload []byte, found bool, err error) {
	leaf, slot, found, err := t.search(key)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return leaf.payload(slot), true, nil
}

func (t *Tree) Insert(rowid int64, payload []byte) error {
	var key [rowidKeySize]byte
	putRowid(key[:], rowid)
	return t.insertKey(key[:], payload)
}

func (t *Tree) InsertKey(key []byte) error {
	return t.insertKey(key, nil)
}

func (t *Tree) insertKey(key []byte, payload []byte) error {
	if cellKeySize(t.kind, key)+binary.MaxVarintLen64+2+len(payload) > pager.PageSize-headerSize {
		return errors.New("btree: record too large for one page (overflow is M7)")
	}
	split, sep, right, err := t.insert(t.root, key, payload)
	if err != nil {
		return err
	}
	if split {
		return t.growRoot(sep, right)
	}
	return nil
}

func (t *Tree) insert(id pager.PageID, key []byte, payload []byte) (split bool, sep []byte, right pager.PageID, err error) {
	n, err := t.load(id)
	if err != nil {
		return false, nil, 0, err
	}
	if n.isLeaf() {
		if err := t.pager.Write(n.page); err != nil {
			return false, nil, 0, err
		}
		if n.insertLeaf(key, payload) {
			return false, nil, 0, nil
		}
		sep, right, err = t.splitLeaf(n, key, payload)
		if err != nil {
			return false, nil, 0, err
		}
		return true, sep, right, nil
	}

	childSplit, sep, right, err := t.insert(n.childPage(key), key, payload)
	if err != nil || !childSplit {
		return false, nil, 0, err
	}
	if err := t.pager.Write(n.page); err != nil {
		return false, nil, 0, err
	}
	if n.insertInterior(sep, right) {
		return false, nil, 0, nil
	}
	sep, right, err = t.splitInterior(n, sep, right)
	return true, sep, right, err
}

func (t *Tree) splitLeaf(full node, key []byte, payload []byte) ([]byte, pager.PageID, error) {
	cells := make([]leafCell, 0, full.numCells()+1)
	inserted := false
	for i := 0; i < full.numCells(); i++ {
		k := full.key(i)
		if !inserted && t.cmp(key, k) < 0 {
			cells = append(cells, leafCell{key, payload})
			inserted = true
		}
		// k is a window into full.page, which the repack below overwrites.
		cells = append(cells, leafCell{bytes.Clone(k), bytes.Clone(full.payload(i))})
	}
	if !inserted {
		cells = append(cells, leafCell{key, payload})
	}

	mid := len(cells) / 2
	sep := cells[mid].key
	oldSibling := full.rightSibling()

	right, err := t.newLeaf()
	if err != nil {
		return nil, 0, err
	}

	initLeaf(full.page, t.kind, t.cmp)
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

func (t *Tree) splitInterior(full node, sep []byte, child pager.PageID) ([]byte, pager.PageID, error) {
	cells := make([]interiorCell, 0, full.numCells()+1)
	inserted := false
	for i := 0; i < full.numCells(); i++ {
		k := full.key(i)
		if !inserted && t.cmp(sep, k) < 0 {
			cells = append(cells, interiorCell{sep, child})
			inserted = true
		}
		cells = append(cells, interiorCell{bytes.Clone(k), full.child(i)})
	}
	if !inserted {
		cells = append(cells, interiorCell{sep, child})
	}

	mid := len(cells) / 2
	median := cells[mid]
	leftmost := full.leftmostChild()

	right, err := t.newInterior(median.child)
	if err != nil {
		return nil, 0, err
	}
	for _, c := range cells[mid+1:] {
		right.insertInterior(c.sep, c.child)
	}

	initInterior(full.page, leftmost, t.kind, t.cmp)
	for _, c := range cells[:mid] {
		full.insertInterior(c.sep, c.child)
	}

	return median.sep, right.page.ID, nil
}

func (t *Tree) growRoot(sep []byte, right pager.PageID) error {
	root, err := t.load(t.root)
	if err != nil {
		return err
	}

	left, err := t.pager.Allocate()
	if err != nil {
		return err
	}
	left.Data = root.page.Data

	if err := t.pager.Write(root.page); err != nil {
		return err
	}
	newRoot := initInterior(root.page, left.ID, t.kind, t.cmp)
	newRoot.insertInterior(sep, right)
	return nil
}

func (t *Tree) MaxRowID() (int64, bool, error) {
	if t.kind != tableTree {
		return 0, false, errors.New("btree: MaxRowID only works on table trees")
	}
	n, err := t.load(t.root)
	if err != nil {
		return 0, false, err
	}
	for !n.isLeaf() {
		n, err = t.load(n.child(n.numCells() - 1))
		if err != nil {
			return 0, false, err
		}
	}
	if n.numCells() == 0 {
		return 0, false, nil
	}
	return rowidOf(n.key(n.numCells() - 1)), true, nil
}

func (t *Tree) Update(rowid int64, payload []byte) (bool, error) {
	deleted, err := t.Delete(rowid)
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, nil
	}
	return true, t.Insert(rowid, payload)
}

func (t *Tree) Delete(rowid int64) (bool, error) {
	var key [rowidKeySize]byte
	putRowid(key[:], rowid)
	return t.deleteKey(key[:])
}

func (t *Tree) DeleteKey(key []byte) (bool, error) {
	return t.deleteKey(key)
}

func (t *Tree) deleteKey(key []byte) (bool, error) {
	leaf, slot, found, err := t.search(key)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if err := t.pager.Write(leaf.page); err != nil {
		return false, err
	}
	leaf.deleteLeaf(slot)
	return true, nil
}

func (t *Tree) Seek(key []byte) *Cursor {
	c := &Cursor{tree: t}
	leaf, slot, _, err := t.search(key)
	if err != nil {
		c.err = err
		return c
	}
	c.leaf = leaf
	c.slot = slot - 1
	return c
}

type leafCell struct {
	key []byte
	val []byte
}

type interiorCell struct {
	sep   []byte
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
	for c.slot >= c.leaf.numCells() {
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
	return true
}
func (c *Cursor) RowID() int64 {
	if c.err != nil {
		return 0
	}
	return rowidOf(c.leaf.key(c.slot))
}

func (c *Cursor) Key() []byte {
	if c.err != nil {
		return nil
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
