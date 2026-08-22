package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/btree"
	"github.com/vatsalpatel/sqlette/internal/pager"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type Row = []values.Value

const schemaPage pager.PageID = 1

type Table struct {
	tree    *btree.Tree
	nextID  int64
	indexes []*Index
}

func (t *Table) Get(id int64) (Row, bool, error) {
	payload, found, err := t.tree.Get(id)
	if err != nil || !found {
		return nil, false, err
	}
	row, err := record.Decode(payload)
	if err != nil {
		return nil, false, err
	}
	return row, true, err
}

func (t *Table) Insert(r Row) (int64, error) {
	for _, ix := range t.indexes {
		if err := ix.checkUnique(r, 0); err != nil {
			return 0, err
		}
	}

	t.nextID++
	if err := t.tree.Insert(t.nextID, record.Encode(r)); err != nil {
		return 0, err
	}
	for _, ix := range t.indexes {
		if err := ix.Insert(r, t.nextID); err != nil {
			return 0, err
		}
	}
	return t.nextID, nil
}

func (t *Table) Update(id int64, r Row) (bool, error) {
	old, found, err := t.Get(id)
	if err != nil || !found {
		return false, err
	}

	var moved []*Index
	for _, ix := range t.indexes {
		if ix.sameKey(old, r) {
			continue
		}
		if err := ix.checkUnique(r, id); err != nil {
			return false, err
		}
		moved = append(moved, ix)
	}

	for _, ix := range moved {
		if _, err := ix.Delete(old, id); err != nil {
			return false, err
		}
	}
	if _, err := t.tree.Update(id, record.Encode(r)); err != nil {
		return false, err
	}
	for _, ix := range moved {
		if err := ix.Insert(r, id); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t *Table) Delete(id int64) (bool, error) {
	old, found, err := t.Get(id)
	if err != nil || !found {
		return false, err
	}
	if _, err := t.tree.Delete(id); err != nil {
		return false, err
	}
	for _, ix := range t.indexes {
		if _, err := ix.Delete(old, id); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t *Table) Scan() Cursor {
	return &btreeCursor{inner: t.tree.Cursor()}
}

func (t *Table) Root() pager.PageID {
	return t.tree.Root()
}

func (t *Table) AddIndex(ix *Index) {
	t.indexes = append(t.indexes, ix)
}

func (t *Table) BuildIndex(ix *Index) error {
	c := t.Scan()
	defer c.Close()
	for c.Next() {
		row, id := c.Row(), c.RowID()
		if err := ix.checkUnique(row, 0); err != nil {
			return err
		}
		if err := ix.Insert(row, id); err != nil {
			return err
		}
	}
	if err := c.Err(); err != nil {
		return err
	}
	t.AddIndex(ix)
	return nil
}

type Store struct {
	pager   *pager.Pager
	tables  map[string]*Table
	indexes map[string]*Index
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = filepath.Join(os.TempDir(), "sqlette.db")
	}
	p, err := pager.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Store{pager: p, tables: make(map[string]*Table), indexes: make(map[string]*Index)}
	if p.Count <= schemaPage {
		if _, err := p.Allocate(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) CreateTable(name string) (*Table, error) {
	name = strings.ToLower(name)
	if table, ok := s.tables[name]; ok {
		return table, nil
	}
	tree, err := btree.Create(s.pager)
	if err != nil {
		return nil, err
	}
	t := &Table{tree: tree}
	s.tables[name] = t
	return t, nil
}

func (s *Store) CreateIndex(name string, columns []int, unique bool) (*Index, error) {
	name = strings.ToLower(name)
	if _, ok := s.indexes[name]; ok {
		return nil, fmt.Errorf("index %s already exists", name)
	}
	tree, err := createIndexTree(s.pager)
	if err != nil {
		return nil, err
	}
	ix := newIndex(tree, columns, unique)
	s.indexes[name] = ix
	return ix, nil
}

func (s *Store) Table(name string) (*Table, bool) {
	name = strings.ToLower(name)
	t, ok := s.tables[name]
	return t, ok
}

func (s *Store) Index(name string) (*Index, bool) {
	name = strings.ToLower(name)
	i, ok := s.indexes[name]
	return i, ok
}

func (s *Store) ReadSchema() ([]byte, error) {
	p, err := s.pager.Get(schemaPage)
	if err != nil {
		return nil, err
	}
	return p.Data[:], nil
}

func (s *Store) WriteSchema(buf []byte) error {
	if len(buf) > pager.PageSize {
		return fmt.Errorf("storage: schema is too large: %d bytes", len(buf))
	}
	p, err := s.pager.Get(schemaPage)
	if err != nil {
		return err
	}
	if err := s.pager.Write(p); err != nil {
		return err
	}
	clear(p.Data[:])
	copy(p.Data[:], buf)
	return nil
}

func (s *Store) AttachTable(name string, root pager.PageID) error {
	tree := btree.Open(s.pager, root)
	max, ok, err := tree.MaxRowID()
	if err != nil {
		return err
	}
	nextID := int64(0)
	if ok {
		nextID = max
	}
	s.tables[strings.ToLower(name)] = &Table{tree: tree, nextID: nextID}
	return nil
}

func (s *Store) AttachIndex(name string, root pager.PageID, columns []int, unique bool) (*Index, error) {
	ix := newIndex(openIndexTree(s.pager, root), columns, unique)
	s.indexes[strings.ToLower(name)] = ix
	return ix, nil
}

func (s *Store) Begin() error  { return s.pager.Begin() }
func (s *Store) Commit() error { return s.pager.Commit() }

func (s *Store) Rollback() error {
	if err := s.pager.Rollback(); err != nil {
		return err
	}
	clear(s.tables) // the engine's reload() rebuilds this from the restored schema
	clear(s.indexes)
	return nil
}

func (s *Store) Close() error {
	return s.pager.Close()
}
