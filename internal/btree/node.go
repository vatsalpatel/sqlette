package btree

import (
	"bytes"
	"encoding/binary"

	"github.com/vatsalpatel/sqlette/internal/pager"
)

// A node is one pager page in slotted-page form. Multi-byte integers are big-endian.
//
//	header (10 bytes)
//	  [0]     type          uint8   leaf=1, interior=2
//	  [1]     unused        uint8
//	  [2:4]   cellCount     uint16
//	  [4:6]   contentStart  uint16  start of the back-packed cell area
//	  [6:10]  extra         uint32  leaf: right-sibling page; interior: leftmost child
//	pointer array
//	  [10 : 10+2*cellCount] uint16 cell offsets, sorted by key
//	cells (packed downward from the end of the page)
//	  leaf cell:           rowid int64 | payloadLen uvarint | payload
//	  interior cell:       separator int64 | child uint32
//    index leaf cell:     keyLen uvarint | key | payloadLen uvarint (always 0)
//    index interior cell: keyLen uvarint | key | child uint32

const (
	leafType          = 1
	interiorType      = 2
	indexLeafType     = 3
	indexInteriorType = 4
	headerSize        = 10
)

const (
	offType         = 0
	offCellCount    = 2
	offContentStart = 4
	offExtra        = 6
)

const rowidKeySize = 8

type Compare func(a, b []byte) int

type treeKind uint8

const (
	tableTree treeKind = iota
	indexTree
)

func (n node) setNumCells(c int) { binary.BigEndian.PutUint16(n.page.Data[offCellCount:], uint16(c)) }
func (n node) freeSpace() int    { return n.contentStart() - (headerSize + 2*n.numCells()) }
func (n node) contentStart() int { return int(binary.BigEndian.Uint16(n.page.Data[offContentStart:])) }
func (n node) setContentStart(off int) {
	binary.BigEndian.PutUint16(n.page.Data[offContentStart:], uint16(off))
}
func (n node) extra() pager.PageID {
	return pager.PageID(binary.BigEndian.Uint32(n.page.Data[offExtra:]))
}
func (n node) setExtra(id pager.PageID) {
	binary.BigEndian.PutUint32(n.page.Data[offExtra:], uint32(id))
}
func (n node) cellOffset(i int) int {
	return int(binary.BigEndian.Uint16(n.page.Data[headerSize+2*i:]))
}
func (n node) setCellOffset(i, off int) {
	binary.BigEndian.PutUint16(n.page.Data[headerSize+2*i:], uint16(off))
}
func (n node) cellKey(off int) (key []byte, next int) {
	if !n.isIndex() {
		return n.page.Data[off : off+rowidKeySize], off + rowidKeySize
	}
	klen, w := binary.Uvarint(n.page.Data[off:])
	start := off + w
	return n.page.Data[start : start+int(klen)], start + int(klen)
}

func (n node) cellKeySize(key []byte) int            { return cellKeySize(n.kind(), key) }
func (n node) putCellKey(dst []byte, key []byte) int { return putCellKey(n.kind(), dst, key) }

func initLeaf(p *pager.Page, k treeKind, cmp Compare) node {
	n := node{p, cmp}
	p.Data[offType] = leafType
	if k == indexTree {
		p.Data[offType] = indexLeafType
	}
	n.setContentStart(pager.PageSize)
	n.setNumCells(0)
	return n
}

func initInterior(p *pager.Page, leftmost pager.PageID, k treeKind, cmp Compare) node {
	n := node{p, cmp}
	p.Data[offType] = interiorType
	if k == indexTree {
		p.Data[offType] = indexInteriorType
	}
	n.setContentStart(pager.PageSize)
	n.setExtra(leftmost)
	n.setNumCells(0)
	return n
}

func (n node) isLeaf() bool {
	t := n.page.Data[offType]
	return t == leafType || t == indexLeafType
}

func (n node) isIndex() bool {
	t := n.page.Data[offType]
	return t == indexLeafType || t == indexInteriorType
}

func (n node) kind() treeKind {
	if n.isIndex() {
		return indexTree
	}
	return tableTree
}

func (n node) numCells() int {
	return int(binary.BigEndian.Uint16(n.page.Data[offCellCount:]))
}

func (n node) key(i int) []byte {
	k, _ := n.cellKey(n.cellOffset(i))
	return k
}

func (n node) payload(i int) []byte {
	_, next := n.cellKey(n.cellOffset(i))
	plen, w := binary.Uvarint(n.page.Data[next:])
	start := next + w
	return n.page.Data[start : start+int(plen)]
}

func (n node) child(i int) pager.PageID {
	_, next := n.cellKey(n.cellOffset(i))
	return pager.PageID(binary.BigEndian.Uint32(n.page.Data[next:]))
}

func (n node) rightSibling() pager.PageID {
	return n.extra()
}

func (n node) setRightSibling(id pager.PageID) {
	n.setExtra(id)
}

func (n node) leftmostChild() pager.PageID {
	return n.extra()
}

func (n node) search(key []byte) (int, bool) {
	lo, hi := 0, n.numCells()
	for lo < hi {
		mid := (lo + hi) / 2
		switch c := n.cmp(n.key(mid), key); {
		case c < 0:
			lo = mid + 1
		case c > 0:
			hi = mid
		default:
			return mid, true
		}
	}
	return lo, false
}

func (n node) childPage(key []byte) pager.PageID {
	i, found := n.search(key)
	if found {
		return n.child(i)
	}
	if i == 0 {
		return n.leftmostChild()
	}
	return n.child(i - 1)
}

func (n node) insertLeaf(key []byte, payload []byte) bool {
	var buf [binary.MaxVarintLen64]byte
	w := binary.PutUvarint(buf[:], uint64(len(payload)))
	ksize := n.cellKeySize(key)
	size := ksize + w + len(payload)
	if n.freeSpace() < size+2 {
		return false
	}

	off := n.contentStart() - size
	n.putCellKey(n.page.Data[off:], key)
	copy(n.page.Data[off+ksize:], buf[:w])
	copy(n.page.Data[off+ksize+w:], payload)
	n.setContentStart(off)

	i, _ := n.search(key)
	c := n.numCells()
	ptr := n.page.Data[headerSize:]
	copy(ptr[2*(i+1):2*(c+1)], ptr[2*i:2*c])
	n.setCellOffset(i, off)
	n.setNumCells(c + 1)

	return true
}

func (n node) insertInterior(sep []byte, child pager.PageID) bool {
	ksize := n.cellKeySize(sep)
	size := ksize + 4
	if n.freeSpace() < size+2 {
		return false
	}

	off := n.contentStart() - size
	n.putCellKey(n.page.Data[off:], sep)
	binary.BigEndian.PutUint32(n.page.Data[off+ksize:], uint32(child))
	n.setContentStart(off)

	i, _ := n.search(sep)
	c := n.numCells()
	ptr := n.page.Data[headerSize:]
	copy(ptr[2*(i+1):2*(c+1)], ptr[2*i:2*c])
	n.setCellOffset(i, off)
	n.setNumCells(c + 1)

	return true
}

func (n node) deleteLeaf(i int) {
	sib := n.rightSibling()
	k := n.kind() // read before initLeaf overwrites the type byte
	var cells []leafCell
	for j := 0; j < n.numCells(); j++ {
		if j != i {
			// Both halves must be cloned: key(j) and payload(j) are windows into
			// the page the repack below is about to overwrite.
			cells = append(cells, leafCell{bytes.Clone(n.key(j)), bytes.Clone(n.payload(j))})
		}
	}

	initLeaf(n.page, k, n.cmp)
	n.setRightSibling(sib)
	for _, c := range cells {
		n.insertLeaf(c.key, c.val)
	}
}

func putRowid(dst []byte, rowid int64) {
	binary.BigEndian.PutUint64(dst, uint64(rowid))
}

func rowidOf(key []byte) int64 {
	return int64(binary.BigEndian.Uint64(key))
}

// cellKeySize and putCellKey must always agree, so they share one rule.
func cellKeySize(k treeKind, key []byte) int {
	if k == indexTree {
		return uvarintLen(len(key)) + len(key)
	}
	return rowidKeySize
}

func putCellKey(k treeKind, dst []byte, key []byte) int {
	if k == indexTree {
		w := binary.PutUvarint(dst, uint64(len(key)))
		return w + copy(dst[w:], key)
	}
	copy(dst, key)
	return rowidKeySize
}

func uvarintLen(v int) int {
	var buf [binary.MaxVarintLen64]byte
	return binary.PutUvarint(buf[:], uint64(v))
}

func compareRowid(a, b []byte) int {
	x, y := rowidOf(a), rowidOf(b)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	default:
		return 0
	}
}
