package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func col(name string) *ast.ColumnRef          { return &ast.ColumnRef{Name: name} }
func lit(k token.Kind, v string) *ast.Literal { return &ast.Literal{Kind: k, Value: v} }
func intLit(v string) *ast.Literal            { return &ast.Literal{Kind: token.INT, Value: v} }

func bin(op token.Kind, l, r ast.Expression) *ast.Binary {
	return &ast.Binary{Op: op, Left: l, Right: r}
}

func un(op token.Kind, e ast.Expression) *ast.Unary {
	return &ast.Unary{Op: op, Operand: e}
}

func mustParse(t *testing.T, src string) ast.Statement {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex(%q): %v", src, err)
	}
	stmt, err := Parse(toks)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return stmt
}

func mustParseExpr(t *testing.T, src string) ast.Expression {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex(%q): %v", src, err)
	}
	p := &parser{toks: toks}
	e, err := p.parseExpr(0)
	if err != nil {
		t.Fatalf("parseExpr(%q): %v", src, err)
	}
	if !p.at(token.EOF) {
		t.Fatalf("parseExpr(%q): stopped early at %s", src, p.peek())
	}
	return e
}

func TestParseSelect(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.SelectStmt
	}{
		{"single column", "SELECT name FROM users", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: col("name")}},
			From:    ast.TableRef{Name: "users"},
		}},
		{"star", "SELECT * FROM users", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: &ast.Star{}}},
			From:    ast.TableRef{Name: "users"},
		}},
		{"column alias", "SELECT id, name AS n FROM users", &ast.SelectStmt{
			Columns: []ast.ResultColumn{
				{Expr: col("id")},
				{Expr: col("name"), Alias: "n"},
			},
			From: ast.TableRef{Name: "users"},
		}},
		{"table alias", "SELECT x FROM users AS u", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: col("x")}},
			From:    ast.TableRef{Name: "users", Alias: "u"},
		}},
		{"where clause", "SELECT name FROM users WHERE age > 30", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: col("name")}},
			From:    ast.TableRef{Name: "users"},
			Where:   bin(token.GT, col("age"), intLit("30")),
		}},
		{"no table", "SELECT 1;", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: intLit("1")}},
			From:    ast.TableRef{Name: ""},
		}},
		{"aliased expression", "SELECT a + b AS sum FROM t", &ast.SelectStmt{
			Columns: []ast.ResultColumn{
				{Expr: bin(token.PLUS, col("a"), col("b")), Alias: "sum"},
			},
			From: ast.TableRef{Name: "t"},
		}},
		{"where with and", "SELECT * FROM t WHERE a > 1 AND b < 2", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: &ast.Star{}}},
			From:    ast.TableRef{Name: "t"},
			Where: bin(token.AND,
				bin(token.GT, col("a"), intLit("1")),
				bin(token.LT, col("b"), intLit("2"))),
		}},
		{"arithmetic", "SELECT age + 1, age - 1, age * 2, age / 2, age % 2, age % 2 + 1 FROM users",
			&ast.SelectStmt{
				Columns: []ast.ResultColumn{
					{Expr: bin(token.PLUS, col("age"), intLit("1"))},
					{Expr: bin(token.MINUS, col("age"), intLit("1"))},
					{Expr: bin(token.STAR, col("age"), intLit("2"))},
					{Expr: bin(token.SLASH, col("age"), intLit("2"))},
					{Expr: bin(token.PERCENT, col("age"), intLit("2"))},
					{Expr: bin(token.PLUS, bin(token.PERCENT, col("age"), intLit("2")), intLit("1"))},
				},
				From: ast.TableRef{Name: "users"},
			},
		},
		{"star plus column", "SELECT *, extra FROM users", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: &ast.Star{}}, {Expr: col("extra")}},
			From:    ast.TableRef{Name: "users"},
		}},
		{"qualified column", "SELECT t.x FROM t", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: &ast.ColumnRef{Table: "t", Name: "x"}}},
			From:    ast.TableRef{Name: "t"},
		}},
		{"qualified *", "SELECT t.* FROM t", &ast.SelectStmt{
			Columns: []ast.ResultColumn{{Expr: &ast.Star{Table: "t"}}},
			From:    ast.TableRef{Name: "t"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParse(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q)\n got: %#v\nwant: %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseLiterals(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want ast.Expression
	}{
		{"int", "42", lit(token.INT, "42")},
		{"float", "3.14", lit(token.FLOAT, "3.14")},
		{"string", "'hello'", lit(token.STRING, "hello")},
		{"null", "NULL", lit(token.NULL, "NULL")},
		{"column ref", "name", col("name")},
		{"quoted ident", `"first name"`, col("first name")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParseExpr(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseExpr(%q)\n got: %#v\nwant: %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseExprPrecedence(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want ast.Expression
	}{
		{"mul binds tighter than add", "1 + 2 * 3",
			bin(token.PLUS, intLit("1"), bin(token.STAR, intLit("2"), intLit("3")))},
		{"add is left associative", "1 + 2 + 3",
			bin(token.PLUS, bin(token.PLUS, intLit("1"), intLit("2")), intLit("3"))},
		{"sub is left associative", "1 - 2 - 3",
			bin(token.MINUS, bin(token.MINUS, intLit("1"), intLit("2")), intLit("3"))},
		{"mul and div share precedence, left assoc", "8 / 4 * 2",
			bin(token.STAR, bin(token.SLASH, intLit("8"), intLit("4")), intLit("2"))},
		{"mixed arithmetic", "2 * 3 + 4 * 5",
			bin(token.PLUS,
				bin(token.STAR, intLit("2"), intLit("3")),
				bin(token.STAR, intLit("4"), intLit("5")))},
		{"and binds tighter than or", "a AND b OR c",
			bin(token.OR, bin(token.AND, col("a"), col("b")), col("c"))},
		{"or is left associative", "a OR b OR c",
			bin(token.OR, bin(token.OR, col("a"), col("b")), col("c"))},
		{"comparison binds tighter than and", "a = b AND c = d",
			bin(token.AND,
				bin(token.EQ, col("a"), col("b")),
				bin(token.EQ, col("c"), col("d")))},
		{"arithmetic binds tighter than comparison", "1 + 2 = 3",
			bin(token.EQ, bin(token.PLUS, intLit("1"), intLit("2")), intLit("3"))},
		{"not equal operator", "a <> b", bin(token.NEQ, col("a"), col("b"))},
		{"less or equal operator", "a <= b", bin(token.LTE, col("a"), col("b"))},
		{"greater or equal operator", "a >= b", bin(token.GTE, col("a"), col("b"))},
		{"not binds tighter than and", "NOT a AND b",
			bin(token.AND, un(token.NOT, col("a")), col("b"))},
		{"not is looser than comparison", "NOT a = b",
			un(token.NOT, bin(token.EQ, col("a"), col("b")))},
		{"nested not", "NOT NOT a",
			un(token.NOT, un(token.NOT, col("a")))},
		{"concat is left associative", "a || b || c",
			bin(token.CONCAT, bin(token.CONCAT, col("a"), col("b")), col("c"))},
		{"unary minus binds tight", "-a + b",
			bin(token.PLUS, un(token.MINUS, col("a")), col("b"))},
		{"unary minus inside product", "2 * -3",
			bin(token.STAR, intLit("2"), un(token.MINUS, intLit("3")))},
		{"parens override precedence", "(1 + 2) * 3",
			bin(token.STAR, bin(token.PLUS, intLit("1"), intLit("2")), intLit("3"))},
		{"parens force grouping", "2 * (3 + 4)",
			bin(token.STAR, intLit("2"), bin(token.PLUS, intLit("3"), intLit("4")))},
		{"redundant parens collapse", "((a))", col("a")},
		// once IS is handled (an infix op that swallows NULL / NOT NULL):
		// {"is null", "a IS NULL",
		// 	bin(token.IS, col("a"), &ast.Literal{Kind: token.NULL, Value: "NULL"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParseExpr(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseExpr(%q)\n got: %#v\nwant: %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"not a statement", "123", "expected a statement"},
		{"missing table name", "SELECT name FROM", "expected IDENT"},
		{"trailing tokens", "SELECT name FROM users 5", "after statement"},
		{"unclosed paren", "SELECT (a FROM t", "expected RPAREN"},
		{"empty input", "", "expected a statement"},
		{"dangling operator", "SELECT a + FROM t", "expected an expression"},
		{"insert missing into", "INSERT users VALUES (1)", "expected INTO"},
		{"insert missing values", "INSERT INTO t", "expected VALUES"},
		{"insert row without parens", "INSERT INTO t VALUES 1", "expected LPAREN"},
		{"create missing table keyword", "CREATE t (id INT)", "expected TABLE"},
		{"create missing columns", "CREATE TABLE t", "expected LPAREN"},
		{"create primary without key", "CREATE TABLE t (id INT PRIMARY)", "expected KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := lexer.Lex(tt.src)
			if err != nil {
				t.Fatalf("lex(%q): %v", tt.src, err)
			}
			if _, err := Parse(toks); err == nil {
				t.Fatalf("Parse(%q): expected an error, got nil", tt.src)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse(%q): error %q does not contain %q", tt.src, err.Error(), tt.want)
			}
		})
	}
}

func TestParseInsert(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.InsertStmt
	}{
		{"with column list", "INSERT INTO users (id, name) VALUES (1, 'alice')", &ast.InsertStmt{
			Table:   "users",
			Columns: []ast.ColumnRef{{Name: "id"}, {Name: "name"}},
			Rows:    [][]ast.Expression{{intLit("1"), lit(token.STRING, "alice")}},
		}},
		{"no column list", "INSERT INTO users VALUES (1, 'bob')", &ast.InsertStmt{
			Table: "users",
			Rows:  [][]ast.Expression{{intLit("1"), lit(token.STRING, "bob")}},
		}},
		{"multiple rows", "INSERT INTO t VALUES (1, 2), (3, 4)", &ast.InsertStmt{
			Table: "t",
			Rows: [][]ast.Expression{
				{intLit("1"), intLit("2")},
				{intLit("3"), intLit("4")},
			},
		}},
		{"expression value", "INSERT INTO t (a) VALUES (1 + 2)", &ast.InsertStmt{
			Table:   "t",
			Columns: []ast.ColumnRef{{Name: "a"}},
			Rows:    [][]ast.Expression{{bin(token.PLUS, intLit("1"), intLit("2"))}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParse(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q)\n got: %#v\nwant: %#v", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseCreate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.CreateStmt
	}{
		{"basic", "CREATE TABLE t (id INT, name TEXT)", &ast.CreateStmt{
			Table: "t",
			Columns: []ast.ColumnDef{
				{Name: "id", Type: "INT"},
				{Name: "name", Type: "TEXT"},
			},
		}},
		{"primary key", "CREATE TABLE users (id INT PRIMARY KEY, name TEXT)", &ast.CreateStmt{
			Table: "users",
			Columns: []ast.ColumnDef{
				{Name: "id", Type: "INT", PrimaryKey: true},
				{Name: "name", Type: "TEXT"},
			},
		}},
		{"not null", "CREATE TABLE t (id INT NOT NULL)", &ast.CreateStmt{
			Table:   "t",
			Columns: []ast.ColumnDef{{Name: "id", Type: "INT", NotNull: true}},
		}},
		{"primary key and not null", "CREATE TABLE t (id INT PRIMARY KEY NOT NULL)", &ast.CreateStmt{
			Table:   "t",
			Columns: []ast.ColumnDef{{Name: "id", Type: "INT", PrimaryKey: true, NotNull: true}},
		}},
		{"not null and primary key", "CREATE TABLE t (id INT NOT NULL PRIMARY KEY)", &ast.CreateStmt{
			Table:   "t",
			Columns: []ast.ColumnDef{{Name: "id", Type: "INT", PrimaryKey: true, NotNull: true}},
		}},
		{"single column", "CREATE TABLE t (id INT)", &ast.CreateStmt{
			Table:   "t",
			Columns: []ast.ColumnDef{{Name: "id", Type: "INT"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParse(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q)\n got: %#v\nwant: %#v", tt.src, got, tt.want)
			}
		})
	}
}
