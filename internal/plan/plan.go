package plan

import (
	"github.com/vatsalpatel/sqlette/internal/ast"
)

type Node interface {
	isNode()
}

type SeqScan struct {
	Table string
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
	Index     string
	Column    string
	Low, High *Bound
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
func (d *Delete) isNode()    {}
func (u *Update) isNode()    {}
