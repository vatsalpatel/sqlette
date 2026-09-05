package parser_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func selectOf(t *testing.T, src string) *ast.SelectStmt {
	t.Helper()
	sel, ok := mustParse(t, src).(*ast.SelectStmt)
	assert.True(t, ok)
	return sel
}

func TestParseOrderBy(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []ast.OrderTerm
	}{
		{"bare", "SELECT a FROM t ORDER BY a", []ast.OrderTerm{
			{Expr: col("a")},
		}},
		{"explicit asc", "SELECT a FROM t ORDER BY a ASC", []ast.OrderTerm{
			{Expr: col("a")},
		}},
		{"desc", "SELECT a FROM t ORDER BY a DESC", []ast.OrderTerm{
			{Expr: col("a"), Desc: true},
		}},
		{"mixed directions", "SELECT a FROM t ORDER BY a DESC, b, c ASC", []ast.OrderTerm{
			{Expr: col("a"), Desc: true},
			{Expr: col("b")},
			{Expr: col("c")},
		}},
		{"expression", "SELECT a FROM t ORDER BY a + 1 DESC", []ast.OrderTerm{
			{Expr: bin(token.PLUS, col("a"), intLit("1")), Desc: true},
		}},
		{"ordinal", "SELECT a, b FROM t ORDER BY 2", []ast.OrderTerm{
			{Expr: intLit("2")},
		}},
		{"qualified", "SELECT a FROM t ORDER BY t.a", []ast.OrderTerm{
			{Expr: &ast.ColumnRef{Table: "t", Name: "a"}},
		}},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			assert.DeepEqual(t, c.want, selectOf(t, c.src).OrderBy)
		})
	}
}

func TestParseLimitAndOffset(t *testing.T) {
	sel := selectOf(t, "SELECT a FROM t LIMIT 5")
	assert.DeepEqual(t, intLit("5"), sel.Limit)
	assert.True(t, sel.Offset == nil)

	sel = selectOf(t, "SELECT a FROM t LIMIT 5 OFFSET 10")
	assert.DeepEqual(t, intLit("5"), sel.Limit)
	assert.DeepEqual(t, intLit("10"), sel.Offset)

	// SQLite reads a negative limit as no limit at all, so the parser has to
	// keep the unary minus rather than reject it.
	sel = selectOf(t, "SELECT a FROM t LIMIT -1")
	assert.DeepEqual(t, un(token.MINUS, intLit("1")), sel.Limit)
}

func TestParseWhereOrderLimitTogether(t *testing.T) {
	sel := selectOf(t, "SELECT a FROM t WHERE a > 1 ORDER BY b DESC LIMIT 2 OFFSET 3")

	assert.DeepEqual(t, bin(token.GT, col("a"), intLit("1")), sel.Where)
	assert.DeepEqual(t, []ast.OrderTerm{{Expr: col("b"), Desc: true}}, sel.OrderBy)
	assert.DeepEqual(t, intLit("2"), sel.Limit)
	assert.DeepEqual(t, intLit("3"), sel.Offset)
}

func TestParseOrderLimitErrors(t *testing.T) {
	for _, src := range []string{
		"SELECT a FROM t ORDER a",
		"SELECT a FROM t ORDER BY",
		"SELECT a FROM t LIMIT",
		"SELECT a FROM t OFFSET 5",
		"SELECT a FROM t LIMIT 5 OFFSET",
	} {
		t.Run(src, func(t *testing.T) {
			toks, err := lexer.Lex(src)
			assert.NoError(t, err)
			if _, err := parser.Parse(toks); err == nil {
				t.Fatalf("%q should not parse", src)
			}
		})
	}
}

// The pretty-printer feeds EXPLAIN and the parser goldens, so the new clauses
// have to survive a round trip through it.
func TestSelectStringIncludesOrderAndLimit(t *testing.T) {
	sel := selectOf(t, "SELECT a FROM t WHERE a > 1 ORDER BY b DESC, a LIMIT 2 OFFSET 3")

	want := "(select (cols a) (from t) (where (> a 1)) (order (desc b) (asc a)) (limit 2) (offset 3))"
	assert.Equal(t, want, sel.String())
}
