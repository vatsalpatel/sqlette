package ast

import "github.com/vatsalpatel/sqlette/internal/token"

type (
	Literal struct {
		Kind  token.Kind
		Value string
	}
	ColumnRef struct {
		Table string
		Name  string
	}
	Star struct {
		Table string
	}
	Unary struct {
		Operand Expression
		Op      token.Kind
	}
	Binary struct {
		Left, Right Expression
		Op          token.Kind
	}
)

func (e *Literal) exprNode()   {}
func (e *ColumnRef) exprNode() {}
func (e *Star) exprNode()      {}
func (e *Unary) exprNode()     {}
func (e *Binary) exprNode()    {}
