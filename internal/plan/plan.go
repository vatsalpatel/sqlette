package plan

import (
	"github.com/vatsalpatel/sqlette/internal/ast"
)

type Node interface {
	isNode()
}

type SeqScan struct {
	Table   string
	Alias   string
	Columns []string
}

type Project struct {
	Input   Node
	Columns []ast.ResultColumn
}

type Filter struct {
	Input     Node
	Predicate ast.Expression
}

type IndexScan struct {
	Table     string
	Alias     string
	Columns   []string
	Index     string
	Column    string
	Low, High *Bound
}

type OneRow struct{}

type Sort struct {
	Input Node
	Keys  []SortKey
}

type SortKey struct {
	Expr ast.Expression
	Desc bool
}

type Limit struct {
	Input  Node
	Count  int64 // -1 for no limit
	Offset int64
}

type Bound struct {
	Value     ast.Expression
	Inclusive bool
}

type Delete struct {
	Input Node
	Table string
}

type Update struct {
	Input Node
	Table string
}

func (s *SeqScan) isNode()   {}
func (p *Project) isNode()   {}
func (f *Filter) isNode()    {}
func (i *IndexScan) isNode() {}
func (o *OneRow) isNode()    {}
func (s *Sort) isNode()      {}
func (l *Limit) isNode()     {}
func (d *Delete) isNode()    {}
func (u *Update) isNode()    {}
