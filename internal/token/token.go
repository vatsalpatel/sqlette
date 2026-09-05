package token

import (
	"fmt"
	"strings"
)

// Kind is the lexical class of a token. The zero value is ILLEGAL so an
// uninitialized Token is never mistaken for something valid.
type Kind int

const (
	ILLEGAL Kind = iota // a byte/sequence the lexer couldn't make sense of
	EOF                 // end of input; always the last token

	// literals & names
	IDENT  // users, age, "quoted ident"
	INT    // 30
	FLOAT  // 3.14, 1e-9
	STRING // 'a string'

	// keywords
	SELECT
	FROM
	WHERE
	INSERT
	INTO
	VALUES
	UPDATE
	SET
	DELETE
	CREATE
	TABLE
	AND
	OR
	NOT
	NULL
	IS
	AS
	PRIMARY
	KEY
	TRUE  // true
	FALSE // false
	EXPLAIN
	QUERY
	PLAN
	BEGIN
	COMMIT
	ROLLBACK
	TRANSACTION
	END
	INDEX
	ON
	UNIQUE
	ORDER
	BY
	ASC
	DESC
	LIMIT
	OFFSET

	// operators & punctuation
	EQ        // =
	NEQ       // <> or !=
	LT        // <
	LTE       // <=
	GT        // >
	GTE       // >=
	PLUS      // +
	MINUS     // -
	STAR      // *
	SLASH     // /
	PERCENT   // %
	CONCAT    // ||
	LPAREN    // (
	RPAREN    // )
	COMMA     // ,
	SEMICOLON // ;
	DOT       // .
)

// Token is one lexical unit. Lexeme holds the token's text; for STRING and
// quoted IDENT it is the decoded value (quotes stripped, "" / ” unescaped),
// not the raw source slice. Pos is the byte offset where the token starts.
type Token struct {
	Kind   Kind
	Lexeme string
	Pos    int
}

func (t Token) String() string {
	switch t.Kind {
	case EOF, ILLEGAL:
		if t.Lexeme == "" {
			return t.Kind.String()
		}
	}
	return fmt.Sprintf("%s(%q)", t.Kind, t.Lexeme)
}

var keywords = map[string]Kind{
	"SELECT":      SELECT,
	"FROM":        FROM,
	"WHERE":       WHERE,
	"INSERT":      INSERT,
	"INTO":        INTO,
	"VALUES":      VALUES,
	"UPDATE":      UPDATE,
	"SET":         SET,
	"DELETE":      DELETE,
	"CREATE":      CREATE,
	"TABLE":       TABLE,
	"AND":         AND,
	"OR":          OR,
	"NOT":         NOT,
	"NULL":        NULL,
	"IS":          IS,
	"AS":          AS,
	"PRIMARY":     PRIMARY,
	"KEY":         KEY,
	"TRUE":        TRUE,
	"FALSE":       FALSE,
	"EXPLAIN":     EXPLAIN,
	"QUERY":       QUERY,
	"PLAN":        PLAN,
	"BEGIN":       BEGIN,
	"COMMIT":      COMMIT,
	"ROLLBACK":    ROLLBACK,
	"TRANSACTION": TRANSACTION,
	"END":         END,
	"INDEX":       INDEX,
	"ON":          ON,
	"UNIQUE":      UNIQUE,
	"ORDER":       ORDER,
	"BY":          BY,
	"ASC":         ASC,
	"DESC":        DESC,
	"LIMIT":       LIMIT,
	"OFFSET":      OFFSET,
}

// Lookup maps a bare identifier to its keyword Kind, or IDENT if it is not a
// keyword. Matching is case-insensitive: select, SELECT and SeLeCt all map to
// SELECT. Callers must NOT run quoted identifiers through this — "select" is an
// identifier, never the keyword.
func Lookup(word string) Kind {
	if k, ok := keywords[strings.ToUpper(word)]; ok {
		return k
	}
	return IDENT
}

var kindNames = map[Kind]string{
	ILLEGAL:     "ILLEGAL",
	EOF:         "EOF",
	IDENT:       "IDENT",
	INT:         "INT",
	FLOAT:       "FLOAT",
	STRING:      "STRING",
	SELECT:      "SELECT",
	FROM:        "FROM",
	WHERE:       "WHERE",
	INSERT:      "INSERT",
	INTO:        "INTO",
	VALUES:      "VALUES",
	UPDATE:      "UPDATE",
	SET:         "SET",
	DELETE:      "DELETE",
	CREATE:      "CREATE",
	TABLE:       "TABLE",
	AND:         "AND",
	OR:          "OR",
	NOT:         "NOT",
	NULL:        "NULL",
	IS:          "IS",
	AS:          "AS",
	PRIMARY:     "PRIMARY",
	KEY:         "KEY",
	TRUE:        "TRUE",
	FALSE:       "FALSE",
	EXPLAIN:     "EXPLAIN",
	QUERY:       "QUERY",
	PLAN:        "PLAN",
	BEGIN:       "BEGIN",
	COMMIT:      "COMMIT",
	ROLLBACK:    "ROLLBACK",
	TRANSACTION: "TRANSACTION",
	END:         "END",
	INDEX:       "INDEX",
	ON:          "ON",
	UNIQUE:      "UNIQUE",
	ORDER:       "ORDER",
	BY:          "BY",
	ASC:         "ASC",
	DESC:        "DESC",
	LIMIT:       "LIMIT",
	OFFSET:      "OFFSET",
	EQ:          "EQ",
	NEQ:         "NEQ",
	LT:          "LT",
	LTE:         "LTE",
	GT:          "GT",
	GTE:         "GTE",
	PLUS:        "PLUS",
	MINUS:       "MINUS",
	STAR:        "STAR",
	SLASH:       "SLASH",
	PERCENT:     "PERCENT",
	CONCAT:      "CONCAT",
	LPAREN:      "LPAREN",
	RPAREN:      "RPAREN",
	COMMA:       "COMMA",
	SEMICOLON:   "SEMICOLON",
	DOT:         "DOT",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}
