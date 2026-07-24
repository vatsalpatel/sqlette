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
	tree   *btree.Tree
	nextID int64
}

func (t *Table) Insert(r Row) (int64, error) {
	t.nextID++
	if err := t.tree.Insert(t.nextID, record.Encode(r)); err != nil {
		return 0, err
	}
	return t.nextID, nil
}

func (t *Table) Scan() Cursor {
	return &btreeCursor{inner: t.tree.Cursor()}
}

func (t *Table) Root() pager.PageID {
	return t.tree.Root()
}

type Store struct {
	pager  *pager.Pager
	tables map[string]*Table
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = filepath.Join(os.TempDir(), "sqlette.db")
	}
	p, err := pager.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Store{pager: p, tables: make(map[string]*Table)}
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

func (s *Store) Table(name string) (*Table, bool) {
	name = strings.ToLower(name)
	t, ok := s.tables[name]
	return t, ok
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

func (s *Store) Begin() error  { return s.pager.Begin() }
func (s *Store) Commit() error { return s.pager.Commit() }

func (s *Store) Rollback() error {
	if err := s.pager.Rollback(); err != nil {
		return err
	}
	clear(s.tables) // the engine's reload() rebuilds this from the restored schema
	return nil
}

func (s *Store) Close() error {
	return s.pager.Close()
}
