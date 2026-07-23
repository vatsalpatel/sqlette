package btree

import (
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
//	  leaf cell:     rowid int64 | payloadLen uvarint | payload
//	  interior cell: separator int64 | child uint32

const (
	leafType     = 1
	interiorType = 2
	headerSize   = 10
)

const (
	offType         = 0
	offCellCount    = 2
	offContentStart = 4
	offExtra        = 6
)

func (n node) setNumCells(c int) { binary.BigEndian.PutUint16(n.page.Data[offCellCount:], uint16(c)) }
func (n node) contentStart() int { return int(binary.BigEndian.Uint16(n.page.Data[offContentStart:])) }
func (n node) freeSpace() int    { return n.contentStart() - (headerSize + 2*n.numCells()) }
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

func initLeaf(p *pager.Page) node {
	n := node{p}
	p.Data[offType] = leafType
	n.setContentStart(pager.PageSize)
	n.setNumCells(0)
	return n
}

func initInterior(p *pager.Page, leftmost pager.PageID) node {
	n := node{p}
	p.Data[offType] = interiorType
	n.setContentStart(pager.PageSize)
	n.setExtra(leftmost)
	n.setNumCells(0)
	return n
}

func (n node) isLeaf() bool {
	return n.page.Data[0] == leafType
}

func (n node) numCells() int {
	return int(binary.BigEndian.Uint16(n.page.Data[offCellCount:]))
}

func (n node) key(i int) int64 {
	off := n.cellOffset(i)
	return int64(binary.BigEndian.Uint64(n.page.Data[off:]))
}

func (n node) payload(i int) []byte {
	off := n.cellOffset(i)
	plen, w := binary.Uvarint(n.page.Data[off+8:])
	start := off + 8 + w
	return n.page.Data[start : start+int(plen)]
}

func (n node) child(i int) pager.PageID {
	off := n.cellOffset(i)
	return pager.PageID(binary.BigEndian.Uint32(n.page.Data[off+8:]))
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

func (n node) search(key int64) (int, bool) {
	lo, hi := 0, n.numCells()
	for lo < hi {
		mid := (lo + hi) / 2
		switch k := n.key(mid); {
		case k < key:
			lo = mid + 1
		case k > key:
			hi = mid
		default:
			return mid, true
		}
	}
	return lo, false
}

func (n node) childPage(key int64) pager.PageID {
	i, found := n.search(key)
	if found {
		return n.child(i)
	}
	if i == 0 {
		return n.leftmostChild()
	}
	return n.child(i - 1)
}

func (n node) insertLeaf(rowid int64, payload []byte) bool {
	var buf [binary.MaxVarintLen64]byte
	w := binary.PutUvarint(buf[:], uint64(len(payload)))
	size := 8 + w + len(payload)
	if n.freeSpace() < size+2 {
		return false
	}

	off := n.contentStart() - size
	binary.BigEndian.PutUint64(n.page.Data[off:], uint64(rowid))
	copy(n.page.Data[off+8:], buf[:w])
	copy(n.page.Data[off+8+w:], payload)
	n.setContentStart(off)

	i, _ := n.search(rowid)
	c := n.numCells()
	ptr := n.page.Data[headerSize:]
	copy(ptr[2*(i+1):2*(c+1)], ptr[2*i:2*c])
	n.setCellOffset(i, off)
	n.setNumCells(c + 1)

	return true
}

func (n node) insertInterior(separator int64, child pager.PageID) bool {
	const size = 8 + 4
	if n.freeSpace() < size+2 {
		return false
	}

	off := n.contentStart() - size
	binary.BigEndian.PutUint64(n.page.Data[off:], uint64(separator))
	binary.BigEndian.PutUint32(n.page.Data[off+8:], uint32(child))
	n.setContentStart(off)

	i, _ := n.search(separator)
	c := n.numCells()
	ptr := n.page.Data[headerSize:]
	copy(ptr[2*(i+1):2*(c+1)], ptr[2*i:2*c])
	n.setCellOffset(i, off)
	n.setNumCells(c + 1)

	return true
}
