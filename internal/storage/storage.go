package storage

import (
	"strings"

	"github.com/vatsalpatel/sqlette/internal/values"
)

type Row = []values.Value

type Table struct {
	rows   []Row
	nextID int64
}

func (t *Table) Insert(r Row) int64 {
	t.nextID++
	t.rows = append(t.rows, r)
	return t.nextID
}

func (t *Table) Scan() Cursor {
	return &heapCursor{heap: t.rows, idx: -1}
}

type Store struct {
	tables map[string]*Table
}

func New() *Store {
	return &Store{tables: make(map[string]*Table)}
}

func (s *Store) CreateTable(name string) *Table {
	name = strings.ToLower(name)
	if table, ok := s.tables[name]; ok {
		return table
	}
	s.tables[name] = &Table{}
	return s.tables[name]
}

func (s *Store) Table(name string) (*Table, bool) {
	name = strings.ToLower(name)
	t, ok := s.tables[name]
	return t, ok
}
