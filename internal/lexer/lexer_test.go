package lexer_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/token"
)

func TestLexTokens(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.Token
	}{
		{"select with where", "SELECT name FROM users WHERE age > 30", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.IDENT, "name"), tok(token.FROM, "FROM"),
			tok(token.IDENT, "users"), tok(token.WHERE, "WHERE"), tok(token.IDENT, "age"),
			tok(token.GT, ">"), tok(token.INT, "30"),
		}},
		{"create table (type names are identifiers)", "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)", []token.Token{
			tok(token.CREATE, "CREATE"), tok(token.TABLE, "TABLE"), tok(token.IDENT, "users"),
			tok(token.LPAREN, "("), tok(token.IDENT, "id"), tok(token.IDENT, "INTEGER"),
			tok(token.PRIMARY, "PRIMARY"), tok(token.KEY, "KEY"), tok(token.COMMA, ","),
			tok(token.IDENT, "name"), tok(token.IDENT, "TEXT"), tok(token.RPAREN, ")"),
		}},
		{"insert values", "INSERT INTO t VALUES (1, 'x', NULL)", []token.Token{
			tok(token.INSERT, "INSERT"), tok(token.INTO, "INTO"), tok(token.IDENT, "t"),
			tok(token.VALUES, "VALUES"), tok(token.LPAREN, "("), tok(token.INT, "1"),
			tok(token.COMMA, ","), tok(token.STRING, "x"), tok(token.COMMA, ","),
			tok(token.NULL, "NULL"), tok(token.RPAREN, ")"),
		}},
		{"boolean expression", "a AND b OR NOT c", []token.Token{
			tok(token.IDENT, "a"), tok(token.AND, "AND"), tok(token.IDENT, "b"),
			tok(token.OR, "OR"), tok(token.NOT, "NOT"), tok(token.IDENT, "c"),
		}},
		{"is not null", "x IS NOT NULL", []token.Token{
			tok(token.IDENT, "x"), tok(token.IS, "IS"), tok(token.NOT, "NOT"), tok(token.NULL, "NULL"),
		}},
		{"qualified name", "users.name", []token.Token{
			tok(token.IDENT, "users"), tok(token.DOT, "."), tok(token.IDENT, "name"),
		}},
		{"arithmetic with concat", "a + b * 2 || c", []token.Token{
			tok(token.IDENT, "a"), tok(token.PLUS, "+"), tok(token.IDENT, "b"),
			tok(token.STAR, "*"), tok(token.INT, "2"), tok(token.CONCAT, "||"), tok(token.IDENT, "c"),
		}},
		{"select star", "SELECT * FROM t", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.STAR, "*"), tok(token.FROM, "FROM"), tok(token.IDENT, "t"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tt.want...)
		})
	}
}

func TestLexKeywordsCaseInsensitive(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"SELECT", token.SELECT}, {"select", token.SELECT}, {"SeLeCt", token.SELECT},
		{"FROM", token.FROM}, {"from", token.FROM},
		{"WHERE", token.WHERE}, {"INSERT", token.INSERT}, {"into", token.INTO},
		{"values", token.VALUES}, {"create", token.CREATE}, {"table", token.TABLE},
		{"and", token.AND}, {"or", token.OR}, {"not", token.NOT}, {"null", token.NULL},
		{"is", token.IS}, {"as", token.AS}, {"primary", token.PRIMARY}, {"key", token.KEY},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			// Kind is case-insensitive; Lexeme preserves the original source text.
			assertTokens(t, tt.src, tok(tt.kind, tt.src))
		})
	}
}

func TestLexIdentifiers(t *testing.T) {
	idents := []string{"users", "_hidden", "col1", "a_b_c", "x9", "ID", "MixedCase"}
	for _, id := range idents {
		t.Run(id, func(t *testing.T) {
			assertTokens(t, id, tok(token.IDENT, id))
		})
	}
}

func TestLexNumbers(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"0", token.INT}, {"30", token.INT}, {"100", token.INT},
		{"3.14", token.FLOAT}, {".5", token.FLOAT}, {"10.", token.FLOAT},
		{"1e9", token.FLOAT}, {"1E9", token.FLOAT}, {"1e-9", token.FLOAT},
		{"1e+9", token.FLOAT}, {"1.5e10", token.FLOAT},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assertTokens(t, tt.src, tok(tt.kind, tt.src))
		})
	}
}

func TestLexStrings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // decoded value
	}{
		{"basic", "'hello'", "hello"},
		{"empty", "''", ""},
		{"escaped quote", "'it''s'", "it's"},
		{"only an escaped quote", "''''", "'"},
		{"spaces", "'a b'", "a b"},
		{"semicolon inside", "'a;b'", "a;b"},
		{"keywords inside", "'SELECT FROM'", "SELECT FROM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tok(token.STRING, tt.want))
		})
	}
}

func TestLexQuotedIdents(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // decoded value
	}{
		{"basic", `"users"`, "users"},
		{"keyword stays an identifier", `"select"`, "select"},
		{"escaped quote", `"a""b"`, `a"b`},
		{"spaces and punctuation", `"my col!"`, "my col!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tok(token.IDENT, tt.want))
		})
	}
}

func TestLexOperators(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"=", token.EQ}, {"<>", token.NEQ}, {"!=", token.NEQ},
		{"<", token.LT}, {"<=", token.LTE}, {">", token.GT}, {">=", token.GTE},
		{"+", token.PLUS}, {"-", token.MINUS}, {"*", token.STAR}, {"/", token.SLASH},
		{"%", token.PERCENT}, {"||", token.CONCAT},
		{"(", token.LPAREN}, {")", token.RPAREN}, {",", token.COMMA},
		{";", token.SEMICOLON}, {".", token.DOT},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			// For operators the lexeme is identical to the source text.
			assertTokens(t, tt.src, tok(tt.kind, tt.src))
		})
	}
}

func TestLexCommentsAndWhitespace(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.Token
	}{
		{"line comment to end", "SELECT 1 -- a trailing comment", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.INT, "1"),
		}},
		{"line comment then newline", "SELECT 1 -- c\nFROM t", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.INT, "1"), tok(token.FROM, "FROM"), tok(token.IDENT, "t"),
		}},
		{"block comment inline", "SELECT/* mid */1", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.INT, "1"),
		}},
		{"block comment multiline", "1 /* a\nb */ 2", []token.Token{
			tok(token.INT, "1"), tok(token.INT, "2"),
		}},
		{"unterminated block runs to EOF (lenient)", "1 /* never closed", []token.Token{
			tok(token.INT, "1"),
		}},
		{"surrounding whitespace", "  \t SELECT\n ", []token.Token{
			tok(token.SELECT, "SELECT"),
		}},
		{"tabs and newlines between tokens", "SELECT\n\t1", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.INT, "1"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tt.want...)
		})
	}
}

func TestLexEmptyInput(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"spaces only", "   "},
		{"newlines and tabs only", "\n\n\t"},
		{"line comment only", "-- just a comment"},
		{"block comment only", "/* nothing here */"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No real tokens — only the implicit trailing EOF, and no error.
			assertTokens(t, tt.src)
		})
	}
}

func TestLexGotchas(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.Token
	}{
		{"double dash after number starts a comment", "5--2", []token.Token{
			tok(token.INT, "5"),
		}},
		{"chained dots split into two floats", "1.2.3", []token.Token{
			tok(token.FLOAT, "1.2"), tok(token.FLOAT, ".3"),
		}},
		{"nested string escapes", "'a''''b'", []token.Token{
			tok(token.STRING, "a''b"),
		}},
		{"no negative-number lexing", "SELECT-1", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.MINUS, "-"), tok(token.INT, "1"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tt.want...)
		})
	}
}

func TestLexPositions(t *testing.T) {
	const src = "SELECT name FROM users WHERE age > 30"
	got, err := lexer.Lex(src)
	assert.NoError(t, err)
	want := []token.Token{
		{Kind: token.SELECT, Lexeme: "SELECT", Pos: 0},
		{Kind: token.IDENT, Lexeme: "name", Pos: 7},
		{Kind: token.FROM, Lexeme: "FROM", Pos: 12},
		{Kind: token.IDENT, Lexeme: "users", Pos: 17},
		{Kind: token.WHERE, Lexeme: "WHERE", Pos: 23},
		{Kind: token.IDENT, Lexeme: "age", Pos: 29},
		{Kind: token.GT, Lexeme: ">", Pos: 33},
		{Kind: token.INT, Lexeme: "30", Pos: 35},
		{Kind: token.EOF, Lexeme: "", Pos: 37},
	}
	assert.Equal(t, len(want), len(got))
	for i := range want {
		assert.Equal(t, want[i], got[i])
	}
}

func TestLexStringPositionUsesRawSpan(t *testing.T) {
	// The decoded lexeme ("it's", 4 bytes) is shorter than the 7-byte source
	// span 'it''s'; the next token's Pos must come from the raw span, not the
	// decoded length.
	const src = "'it''s' x"
	got, err := lexer.Lex(src)
	assert.NoError(t, err)
	want := []token.Token{
		{Kind: token.STRING, Lexeme: "it's", Pos: 0},
		{Kind: token.IDENT, Lexeme: "x", Pos: 8},
		{Kind: token.EOF, Lexeme: "", Pos: 9},
	}
	assert.Equal(t, len(want), len(got))
	for i := range want {
		assert.Equal(t, want[i], got[i])
	}
}

func TestLexErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		msg  string         // substring expected in the error
		want []token.Token  // tokens including any ILLEGAL, excluding EOF
	}{
		{"unterminated string", "'oops", "unterminated string literal", []token.Token{
			tok(token.ILLEGAL, "'oops"),
		}},
		{"unterminated quoted ident", `"oops`, "unterminated quoted identifier", []token.Token{
			tok(token.ILLEGAL, `"oops`),
		}},
		{"number with trailing letters", "12abc", "malformed number", []token.Token{
			tok(token.ILLEGAL, "12abc"),
		}},
		{"hex is unsupported", "0x1F", "malformed number", []token.Token{
			tok(token.ILLEGAL, "0x1F"),
		}},
		{"exponent without digits", "1e", "exponent has no digits", []token.Token{
			tok(token.ILLEGAL, "1e"),
		}},
		{"exponent sign without digits", "1e+", "exponent has no digits", []token.Token{
			tok(token.ILLEGAL, "1e+"),
		}},
		{"lone bang", "!", "did you mean '!='", []token.Token{
			tok(token.ILLEGAL, "!"),
		}},
		{"lone pipe", "|", "did you mean '||'", []token.Token{
			tok(token.ILLEGAL, "|"),
		}},
		{"unexpected character", "@", "unexpected character", []token.Token{
			tok(token.ILLEGAL, "@"),
		}},
		{"recovery keeps lexing after a bad token", "SELECT 12abc FROM t", "malformed number", []token.Token{
			tok(token.SELECT, "SELECT"), tok(token.ILLEGAL, "12abc"),
			tok(token.FROM, "FROM"), tok(token.IDENT, "t"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertError(t, tt.src, tt.msg, tt.want...)
		})
	}
}

func tok(k token.Kind, lexeme string) token.Token {
	return token.Token{Kind: k, Lexeme: lexeme}
}

// assertTokens lexes src, requires success, and checks the produced tokens
// (Kind and Lexeme only, ignoring Pos) equal want followed by an implicit EOF.
func assertTokens(t *testing.T, src string, want ...token.Token) {
	t.Helper()
	got, err := lexer.Lex(src)
	assert.NoError(t, err)
	assertStream(t, got, want)
}

// assertError lexes src, requires an error containing msg, and checks the tokens
// (including any ILLEGAL) equal want followed by EOF — proving error recovery.
func assertError(t *testing.T, src, msg string, want ...token.Token) {
	t.Helper()
	got, err := lexer.Lex(src)
	assert.ErrorContains(t, err, msg)
	assertStream(t, got, want)
}

// assertStream verifies got ends in EOF and its non-EOF tokens match want by
// Kind and Lexeme. want lists the real tokens only; the trailing EOF is implied.
func assertStream(t *testing.T, got, want []token.Token) {
	t.Helper()
	assert.True(t, len(got) > 0)
	assert.Equal(t, token.EOF, got[len(got)-1].Kind)
	got = got[:len(got)-1]
	assert.Equal(t, len(want), len(got))
	for i := range want {
		assert.Equal(t, want[i].Kind, got[i].Kind)
		assert.Equal(t, want[i].Lexeme, got[i].Lexeme)
	}
}

func TestLexTransactionStatements(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []token.Token
	}{
		{"begin", "BEGIN", []token.Token{tok(token.BEGIN, "BEGIN")}},
		{"begin transaction", "BEGIN TRANSACTION", []token.Token{
			tok(token.BEGIN, "BEGIN"), tok(token.TRANSACTION, "TRANSACTION"),
		}},
		{"commit", "COMMIT", []token.Token{tok(token.COMMIT, "COMMIT")}},
		{"commit transaction", "COMMIT TRANSACTION", []token.Token{
			tok(token.COMMIT, "COMMIT"), tok(token.TRANSACTION, "TRANSACTION"),
		}},
		{"end", "END", []token.Token{tok(token.END, "END")}},
		{"end transaction", "END TRANSACTION", []token.Token{
			tok(token.END, "END"), tok(token.TRANSACTION, "TRANSACTION"),
		}},
		{"rollback", "ROLLBACK", []token.Token{tok(token.ROLLBACK, "ROLLBACK")}},
		{"rollback transaction", "ROLLBACK TRANSACTION", []token.Token{
			tok(token.ROLLBACK, "ROLLBACK"), tok(token.TRANSACTION, "TRANSACTION"),
		}},
		{"statement terminated", "BEGIN;", []token.Token{
			tok(token.BEGIN, "BEGIN"), tok(token.SEMICOLON, ";"),
		}},
		{"lowercase", "begin", []token.Token{tok(token.BEGIN, "begin")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTokens(t, tt.src, tt.want...)
		})
	}
}

func TestLexTransactionKeywordsCaseInsensitive(t *testing.T) {
	tests := []struct {
		src  string
		kind token.Kind
	}{
		{"BEGIN", token.BEGIN}, {"begin", token.BEGIN}, {"BeGiN", token.BEGIN},
		{"COMMIT", token.COMMIT}, {"commit", token.COMMIT},
		{"ROLLBACK", token.ROLLBACK}, {"rollback", token.ROLLBACK},
		{"TRANSACTION", token.TRANSACTION}, {"transaction", token.TRANSACTION},
		{"END", token.END}, {"end", token.END},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assertTokens(t, tt.src, tok(tt.kind, tt.src))
		})
	}
}

// The new keywords are reserved, so they no longer lex as bare identifiers.
// Quoting is the escape hatch: Lookup is only applied to bare words, so a
// column really named "end" stays reachable.
func TestLexQuotedTransactionKeywordsStayIdentifiers(t *testing.T) {
	assertTokens(t, `"end"`, tok(token.IDENT, "end"))
	assertTokens(t, `"begin"`, tok(token.IDENT, "begin"))
	assertTokens(t, `SELECT "end" FROM "transaction"`, []token.Token{
		tok(token.SELECT, "SELECT"), tok(token.IDENT, "end"),
		tok(token.FROM, "FROM"), tok(token.IDENT, "transaction"),
	}...)
}

// Words that merely start with a keyword must not be split or reclassified.
func TestLexIdentifiersResemblingTransactionKeywords(t *testing.T) {
	assertTokens(t, "beginning", tok(token.IDENT, "beginning"))
	assertTokens(t, "committed", tok(token.IDENT, "committed"))
	assertTokens(t, "ending", tok(token.IDENT, "ending"))
}
