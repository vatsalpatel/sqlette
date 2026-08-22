package parser_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
)

func TestParseCreateIndex(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.CreateIndexStmt
	}{
		{"single column", "CREATE INDEX idx_name ON users (name)", &ast.CreateIndexStmt{
			Name:    "idx_name",
			Table:   "users",
			Columns: []string{"name"},
		}},
		{"multiple columns", "CREATE INDEX idx_ab ON t (a, b, c)", &ast.CreateIndexStmt{
			Name:    "idx_ab",
			Table:   "t",
			Columns: []string{"a", "b", "c"},
		}},
		// UNIQUE comes before INDEX, which is the one bit of this grammar that
		// trips people up: it is not a column constraint here.
		{"unique", "CREATE UNIQUE INDEX idx_email ON users (email)", &ast.CreateIndexStmt{
			Name:    "idx_email",
			Table:   "users",
			Columns: []string{"email"},
			Unique:  true,
		}},
		{"unique multi column", "CREATE UNIQUE INDEX u ON t (a, b)", &ast.CreateIndexStmt{
			Name:    "u",
			Table:   "t",
			Columns: []string{"a", "b"},
			Unique:  true,
		}},
		{"lowercase keywords", "create unique index u on t (a)", &ast.CreateIndexStmt{
			Name:    "u",
			Table:   "t",
			Columns: []string{"a"},
			Unique:  true,
		}},
		{"quoted identifiers", `CREATE INDEX "my idx" ON "my table" ("my col")`, &ast.CreateIndexStmt{
			Name:    "my idx",
			Table:   "my table",
			Columns: []string{"my col"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
		})
	}
}

func TestParseCreateIndexErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"missing index name", "CREATE INDEX ON t (a)", "expected IDENT"},
		{"missing ON", "CREATE INDEX idx t (a)", "expected ON"},
		{"missing table", "CREATE INDEX idx ON (a)", "expected IDENT"},
		{"missing columns", "CREATE INDEX idx ON t", "expected LPAREN"},
		{"empty column list", "CREATE INDEX idx ON t ()", "expected IDENT"},
		{"unclosed paren", "CREATE INDEX idx ON t (a", "expected RPAREN"},
		// UNIQUE is only meaningful in front of INDEX. Silently dropping it would
		// turn a typo into a table with no constraint on it.
		{"unique table", "CREATE UNIQUE TABLE t (a INT)", "INDEX"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := lexer.Lex(tt.src)
			assert.NoError(t, err)
			_, err = parser.Parse(toks)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
