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

func (s *SeqScan) isNode() {}
func (p *Project) isNode() {}
func (f *Filter) isNode()  {}
