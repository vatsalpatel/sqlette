package ast

import (
	"fmt"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/token"
)

func (l *Literal) String() string {
	if l.Kind == token.STRING {
		return "'" + l.Value + "'"
	}
	return l.Value
}

func (c *ColumnRef) String() string {
	if c.Table != "" {
		return c.Table + "." + c.Name
	}
	return c.Name
}

func (s *Star) String() string {
	if s.Table != "" {
		return s.Table + ".*"
	}
	return "*"
}

func (u *Unary) String() string {
	return fmt.Sprintf("(%s %v)", opSym(u.Op), u.Operand)
}

func (b *Binary) String() string {
	return fmt.Sprintf("(%s %v %v)", opSym(b.Op), b.Left, b.Right)
}

func (c ResultColumn) String() string {
	if c.Alias != "" {
		return fmt.Sprintf("(as %v %s)", c.Expr, c.Alias)
	}
	return fmt.Sprintf("%v", c.Expr)
}

func (t TableRef) String() string {
	if t.Alias != "" {
		return fmt.Sprintf("(as %s %s)", t.Name, t.Alias)
	}
	return t.Name
}

func (s *SelectStmt) String() string {
	cols := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		cols[i] = c.String()
	}
	out := "(select (cols " + strings.Join(cols, " ") + ")"
	if s.From.Name != "" {
		out += " (from " + s.From.String() + ")"
	}
	if s.Where != nil {
		out += fmt.Sprintf(" (where %v)", s.Where)
	}
	if len(s.OrderBy) > 0 {
		terms := make([]string, len(s.OrderBy))
		for i, o := range s.OrderBy {
			dir := "asc"
			if o.Desc {
				dir = "desc"
			}
			terms[i] = fmt.Sprintf("(%s %v)", dir, o.Expr)
		}
		out += " (order " + strings.Join(terms, " ") + ")"
	}
	if s.Limit != nil {
		out += fmt.Sprintf(" (limit %v)", s.Limit)
	}
	if s.Offset != nil {
		out += fmt.Sprintf(" (offset %v)", s.Offset)
	}
	return out + ")"
}

func opSym(k token.Kind) string {
	switch k {
	case token.PLUS:
		return "+"
	case token.MINUS:
		return "-"
	case token.STAR:
		return "*"
	case token.SLASH:
		return "/"
	case token.PERCENT:
		return "%"
	case token.CONCAT:
		return "||"
	case token.EQ:
		return "="
	case token.NEQ:
		return "<>"
	case token.LT:
		return "<"
	case token.LTE:
		return "<="
	case token.GT:
		return ">"
	case token.GTE:
		return ">="
	default:
		return k.String() // AND, OR, NOT, IS
	}
}
