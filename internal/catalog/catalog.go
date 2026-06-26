package catalog

import (
	"fmt"
	"strings"
)

type Catalog struct {
	Tables map[string]*Table
}

type Table struct {
	Name    string
	Columns []Column
}

type Column struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

func New() *Catalog {
	return &Catalog{
		Tables: make(map[string]*Table),
	}
}

func (c *Catalog) Create(t *Table) error {
	t.Name = strings.ToLower(t.Name)
	if _, ok := c.Tables[t.Name]; ok {
		return fmt.Errorf("table %s already exists", t.Name)
	}
	c.Tables[t.Name] = t
	return nil
}

func (c *Catalog) Get(name string) (*Table, bool) {
	t, ok := c.Tables[strings.ToLower(name)]
	return t, ok
}

func (t *Table) ColumnIndex(name string) (int, bool) {
	for i, c := range t.Columns {
		if strings.EqualFold(c.Name, name) {
			return i, true
		}
	}
	return -1, false
}
