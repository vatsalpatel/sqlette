package catalog

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/pager"
)

type Catalog struct {
	Tables  map[string]*Table
	Indexes map[string]*Index
}

type Table struct {
	Name     string
	Columns  []Column
	RootPage pager.PageID
}

type Index struct {
	Name, Table string
	Columns     []string
	Unique      bool
	RootPage    pager.PageID
}

type Column struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

func New() *Catalog {
	return &Catalog{
		Tables:  make(map[string]*Table),
		Indexes: make(map[string]*Index),
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

func (c *Catalog) CreateIndex(i *Index) error {
	i.Name = strings.ToLower(i.Name)
	if _, ok := c.Indexes[i.Name]; ok {
		return fmt.Errorf("index %s already exists", i.Name)
	}
	c.Indexes[i.Name] = i
	return nil
}

func (c *Catalog) Get(name string) (*Table, bool) {
	t, ok := c.Tables[strings.ToLower(name)]
	return t, ok
}

func (c *Catalog) GetIndex(name string) (*Index, bool) {
	i, ok := c.Indexes[strings.ToLower(name)]
	return i, ok
}

func (c *Catalog) IndexesFor(table string) []*Index {
	var indexes []*Index
	for _, i := range c.Indexes {
		if strings.EqualFold(i.Table, table) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (t *Table) ColumnIndex(name string) (int, bool) {
	for i, c := range t.Columns {
		if strings.EqualFold(c.Name, name) {
			return i, true
		}
	}
	return -1, false
}

func (c *Catalog) Marshal() []byte {
	buf := binary.AppendUvarint(nil, uint64(len(c.Tables)))
	for _, t := range c.Tables {
		buf = appendString(buf, t.Name)
		buf = binary.AppendUvarint(buf, uint64(t.RootPage))
		buf = binary.AppendUvarint(buf, uint64(len(t.Columns)))
		for _, c := range t.Columns {
			buf = appendString(buf, c.Name)
			buf = appendString(buf, c.Type)
			var flag byte
			if c.PrimaryKey {
				flag |= 1
			}
			if c.NotNull {
				flag |= 2
			}
			buf = append(buf, flag)
		}
	}
	buf = binary.AppendUvarint(buf, uint64(len(c.Indexes)))
	for _, i := range c.Indexes {
		buf = appendString(buf, i.Name)
		buf = appendString(buf, i.Table)
		buf = binary.AppendUvarint(buf, uint64(i.RootPage))
		var flag byte
		if i.Unique {
			flag |= 1
		}
		buf = append(buf, flag)
		buf = binary.AppendUvarint(buf, uint64(len(i.Columns)))
		for _, c := range i.Columns {
			buf = appendString(buf, c)
		}
	}
	return buf
}

func appendString(buf []byte, s string) []byte {
	buf = binary.AppendUvarint(buf, uint64(len(s)))
	return append(buf, s...)
}

func (c *Catalog) Unmarshal(buf []byte) error {
	tableCount, n2 := binary.Uvarint(buf)
	if n2 <= 0 {
		return fmt.Errorf("invalid catalog length: %v", buf)
	}
	buf = buf[n2:]
	c.Tables = make(map[string]*Table, tableCount)
	for range tableCount {
		name, n1 := readString(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid table name: %v", buf)
		}
		buf = buf[n1:]
		rootPage, n1 := binary.Uvarint(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid root page: %v", buf)
		}
		buf = buf[n1:]
		columnCount, n1 := binary.Uvarint(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid column count: %v", buf)
		}
		buf = buf[n1:]
		t := &Table{
			Name:     name,
			Columns:  make([]Column, columnCount),
			RootPage: pager.PageID(rootPage),
		}
		columns := make([]Column, columnCount)
		for i := range columnCount {
			name, n1 := readString(buf)
			if n1 <= 0 {
				return fmt.Errorf("invalid column name: %v", buf)
			}
			buf = buf[n1:]
			typ, n1 := readString(buf)
			if n1 <= 0 {
				return fmt.Errorf("invalid column type: %v", buf)
			}
			buf = buf[n1:]
			if len(buf) < 1 {
				return fmt.Errorf("invalid column flag: %v", buf)
			}
			flag := buf[0]
			buf = buf[1:]
			columns[i] = Column{
				Name:       name,
				Type:       typ,
				PrimaryKey: flag&1 != 0,
				NotNull:    flag&2 != 0,
			}
		}
		t.Columns = columns
		c.Tables[name] = t
	}
	indexCount, n2 := binary.Uvarint(buf)
	if n2 <= 0 {
		return fmt.Errorf("invalid index count: %v", buf)
	}
	buf = buf[n2:]
	c.Indexes = make(map[string]*Index, indexCount)
	for range indexCount {
		name, n1 := readString(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid index name: %v", buf)
		}
		buf = buf[n1:]

		tbl, n1 := readString(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid table name: %v", buf)
		}
		buf = buf[n1:]

		rootPage, n1 := binary.Uvarint(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid root page: %v", buf)
		}
		buf = buf[n1:]

		if len(buf) < 1 {
			return fmt.Errorf("invalid index flag: %v", buf)
		}
		flag := buf[0]
		buf = buf[1:]

		columnCount, n1 := binary.Uvarint(buf)
		if n1 <= 0 {
			return fmt.Errorf("invalid column count: %v", buf)
		}
		buf = buf[n1:]

		ix := &Index{
			Name:     name,
			Table:    tbl,
			Columns:  make([]string, columnCount),
			RootPage: pager.PageID(rootPage),
			Unique:   flag&1 != 0,
		}
		for i := range columnCount {
			name, n1 := readString(buf)
			if n1 <= 0 {
				return fmt.Errorf("invalid column name: %v", buf)
			}
			buf = buf[n1:]
			ix.Columns[i] = name
		}
		c.Indexes[name] = ix
	}
	return nil
}

func readString(buf []byte) (string, int) {
	length, n := binary.Uvarint(buf)
	if n <= 0 || length > uint64(len(buf)-n) {
		return "", 0
	}
	return string(buf[n : n+int(length)]), n + int(length)
}
