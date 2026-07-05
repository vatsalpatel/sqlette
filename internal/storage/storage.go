package storage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/btree"
	"github.com/vatsalpatel/sqlette/internal/pager"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type Row = []values.Value

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
	return &Store{pager: p, tables: make(map[string]*Table)}, nil
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
	return t, s.pager.Flush()
}

func (s *Store) Table(name string) (*Table, bool) {
	name = strings.ToLower(name)
	t, ok := s.tables[name]
	return t, ok
}
