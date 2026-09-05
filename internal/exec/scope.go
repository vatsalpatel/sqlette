package exec

import (
	"fmt"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/ast"
)

type Column struct {
	Table string
	Name  string
}

type Scope []Column

func (s Scope) Resolve(ref *ast.ColumnRef) (int, error) {
	found := -1
	for i, c := range s {
		if !strings.EqualFold(c.Name, ref.Name) {
			continue
		}
		if ref.Table != "" && !strings.EqualFold(c.Table, ref.Table) {
			continue
		}
		if found >= 0 {
			return -1, fmt.Errorf("ambiguous column %s", ref.Name)
		}
		found = i
	}
	if found < 0 {
		return -1, fmt.Errorf("column %s not found", ref.Name)
	}
	return found, nil
}

func (s Scope) Expand(star *ast.Star) ([]int, error) {
	var out []int
	for i, c := range s {
		if star.Table == "" || strings.EqualFold(c.Table, star.Table) {
			out = append(out, i)
		}
	}
	if out == nil {
		if star.Table == "" {
			return nil, fmt.Errorf("no table specified")
		}
		return nil, fmt.Errorf("no such table %s", star.Table)
	}
	return out, nil
}

func scanScope(table, alias string, cols []string) Scope {
	qualifier := table
	if alias != "" {
		qualifier = alias
	}
	s := make(Scope, len(cols))
	for i, c := range cols {
		s[i] = Column{Table: qualifier, Name: c}
	}
	return s
}
