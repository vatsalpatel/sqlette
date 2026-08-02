package parser_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
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
	assert.NoError(t, err)
	stmt, err := parser.Parse(toks)
	assert.NoError(t, err)
	return stmt
}

func mustParseExpr(t *testing.T, src string) ast.Expression {
	t.Helper()
	stmt := mustParse(t, "SELECT "+src)
	sel, ok := stmt.(*ast.SelectStmt)
	assert.True(t, ok)
	assert.Equal(t, 1, len(sel.Columns))
	return sel.Columns[0].Expr
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
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
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
			assert.DeepEqual(t, tt.want, mustParseExpr(t, tt.src))
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
		{"is null", "a IS NULL",
			bin(token.IS, col("a"), &ast.Literal{Kind: token.NULL, Value: "NULL"})},
		{"is not null", "a IS NOT NULL",
			un(token.NOT, bin(token.IS, col("a"), &ast.Literal{Kind: token.NULL, Value: "NULL"}))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, mustParseExpr(t, tt.src))
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
			assert.NoError(t, err)
			_, err = parser.Parse(toks)
			assert.ErrorContains(t, err, tt.want)
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
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
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
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
		})
	}
}

func TestParseTransactionStatements(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want ast.Statement
	}{
		{"begin", "BEGIN", &ast.BeginStmt{}},
		{"begin transaction", "BEGIN TRANSACTION", &ast.BeginStmt{}},
		{"commit", "COMMIT", &ast.CommitStmt{}},
		{"commit transaction", "COMMIT TRANSACTION", &ast.CommitStmt{}},
		{"rollback", "ROLLBACK", &ast.RollbackStmt{}},
		{"rollback transaction", "ROLLBACK TRANSACTION", &ast.RollbackStmt{}},

		// END is a COMMIT synonym: the parser resolves the spelling so nothing
		// downstream ever has to care which one was typed.
		{"end", "END", &ast.CommitStmt{}},
		{"end transaction", "END TRANSACTION", &ast.CommitStmt{}},

		{"semicolon terminated", "BEGIN;", &ast.BeginStmt{}},
		{"lowercase begin", "begin", &ast.BeginStmt{}},
		{"lowercase commit", "commit", &ast.CommitStmt{}},
		{"lowercase rollback", "rollback", &ast.RollbackStmt{}},
		{"lowercase end", "end", &ast.CommitStmt{}},
		{"mixed case with noise word", "BeGiN tRaNsAcTiOn", &ast.BeginStmt{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
		})
	}
}

// The three statements must stay distinguishable by type — this is what
// engine.Exec will switch on in Stage H.
func TestParseTransactionStatementTypes(t *testing.T) {
	_, ok := mustParse(t, "BEGIN").(*ast.BeginStmt)
	assert.True(t, ok)

	_, ok = mustParse(t, "COMMIT").(*ast.CommitStmt)
	assert.True(t, ok)

	_, ok = mustParse(t, "END").(*ast.CommitStmt)
	assert.True(t, ok)

	_, ok = mustParse(t, "ROLLBACK").(*ast.RollbackStmt)
	assert.True(t, ok)
}

func TestParseUpdate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.UpdateStmt
	}{
		{"single assignment", "UPDATE t SET a = 1", &ast.UpdateStmt{
			Table:   "t",
			Assigns: []ast.Assign{{Column: "a", Value: intLit("1")}},
		}},
		{"string value", "UPDATE users SET name = 'bob'", &ast.UpdateStmt{
			Table:   "users",
			Assigns: []ast.Assign{{Column: "name", Value: lit(token.STRING, "bob")}},
		}},
		{"multiple assignments", "UPDATE t SET a = 1, b = 2", &ast.UpdateStmt{
			Table: "t",
			Assigns: []ast.Assign{
				{Column: "a", Value: intLit("1")},
				{Column: "b", Value: intLit("2")},
			},
		}},
		{"with where", "UPDATE users SET name = 'bob' WHERE id = 1", &ast.UpdateStmt{
			Table:   "users",
			Assigns: []ast.Assign{{Column: "name", Value: lit(token.STRING, "bob")}},
			Where:   bin(token.EQ, col("id"), intLit("1")),
		}},
		{"expression rhs", "UPDATE t SET n = n + 1 WHERE n > 0", &ast.UpdateStmt{
			Table:   "t",
			Assigns: []ast.Assign{{Column: "n", Value: bin(token.PLUS, col("n"), intLit("1"))}},
			Where:   bin(token.GT, col("n"), intLit("0")),
		}},
		// RHS values read the pre-update row, so the parser must preserve
		// assignment order for the executor to evaluate a swap correctly.
		{"swap preserves order", "UPDATE t SET a = b, b = a", &ast.UpdateStmt{
			Table: "t",
			Assigns: []ast.Assign{
				{Column: "a", Value: col("b")},
				{Column: "b", Value: col("a")},
			},
		}},
		{"lowercase keywords", "update t set a = 1", &ast.UpdateStmt{
			Table:   "t",
			Assigns: []ast.Assign{{Column: "a", Value: intLit("1")}},
		}},
		{"semicolon terminated", "UPDATE t SET a = 1;", &ast.UpdateStmt{
			Table:   "t",
			Assigns: []ast.Assign{{Column: "a", Value: intLit("1")}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
		})
	}
}

// UPDATE requires a SET clause and an '=' per assignment. A bare table or a
// column with no value must fail loudly rather than parse to a no-op update.
func TestParseUpdateErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"missing table", "UPDATE SET a = 1", "expected IDENT"},
		{"missing set", "UPDATE t", "expected SET"},
		{"assignment missing equals", "UPDATE t SET a", "expected EQ"},
		{"assignment missing value", "UPDATE t SET a =", "expected an expression"},
		{"trailing comma", "UPDATE t SET a = 1,", "expected IDENT"},
		{"where without predicate", "UPDATE t SET a = 1 WHERE", "expected an expression"},
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

func TestParseDelete(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want *ast.DeleteStmt
	}{
		{"all rows", "DELETE FROM t", &ast.DeleteStmt{Table: "t"}},
		{"with where", "DELETE FROM users WHERE id = 1", &ast.DeleteStmt{
			Table: "users",
			Where: bin(token.EQ, col("id"), intLit("1")),
		}},
		{"compound where", "DELETE FROM t WHERE a > 1 AND b < 2", &ast.DeleteStmt{
			Table: "t",
			Where: bin(token.AND,
				bin(token.GT, col("a"), intLit("1")),
				bin(token.LT, col("b"), intLit("2"))),
		}},
		{"lowercase keywords", "delete from t", &ast.DeleteStmt{Table: "t"}},
		{"semicolon terminated", "DELETE FROM t;", &ast.DeleteStmt{Table: "t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, mustParse(t, tt.src))
		})
	}
}

// DELETE requires FROM and a table name; the WHERE predicate, when present,
// must be a real expression. A bare DELETE or a missing table must fail loudly.
func TestParseDeleteErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"missing from", "DELETE t", "expected FROM"},
		{"missing table", "DELETE FROM", "expected IDENT"},
		{"missing table before where", "DELETE FROM WHERE id = 1", "expected IDENT"},
		{"where without predicate", "DELETE FROM t WHERE", "expected an expression"},
		{"trailing tokens", "DELETE FROM t junk", "after statement"},
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

// Transaction syntax we deliberately do not support must fail loudly rather
// than parse and silently ignore the modifier: BEGIN's isolation modes (we are
// single-writer with no lock modes) and savepoints.
func TestParseUnsupportedTransactionSyntax(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"begin deferred", "BEGIN DEFERRED", "after statement"},
		{"begin immediate", "BEGIN IMMEDIATE", "after statement"},
		{"begin exclusive", "BEGIN EXCLUSIVE TRANSACTION", "after statement"},
		{"rollback to savepoint", "ROLLBACK TO SAVEPOINT sp", "after statement"},
		{"commit with junk", "COMMIT TABLE", "after statement"},
		{"transaction alone", "TRANSACTION", "expected a statement"},
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
