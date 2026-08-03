package lexer

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/token"
)

type Error struct {
	Pos int
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("lex error at offset %d: %s", e.Pos, e.Msg)
}

// The "batch" model lexer: scan everything, then hand a slice to the parser).
// The returned slice always ends with an EOF token, even on error.
// On a malformed token the lexer emits an ILLEGAL token, records
// the error, and keeps going; the returned error joins all of them,
// so one bad token never hides the rest of the input.
func Lex(src string) ([]token.Token, error) {
	l := &lexer{
		src:    src,
		tokens: make([]token.Token, 0, len(src)/2+5),
	}
	for {
		l.skipTrivia()
		if l.atEnd() {
			break
		}
		l.scanToken()
	}
	l.add(token.EOF, l.pos, "")
	return l.tokens, errors.Join(l.errs...)
}

type lexer struct {
	src    string
	pos    int // offset of the next byte to read
	tokens []token.Token
	errs   []error
}

func (l *lexer) scanToken() {
	start := l.pos
	c := l.peek()
	switch {
	case isDigit(c) || (c == '.' && isDigit(l.peekNext())):
		l.scanNumber(start)
	case isAlpha(c):
		l.scanWord(start)
	case c == '\'':
		l.scanString(start)
	case c == '"':
		l.scanQuotedIdent(start)
	default:
		l.scanSymbol(start)
	}
}

func (l *lexer) scanNumber(start int) {
	for isDigit(l.peek()) {
		l.advance()
	}
	isFloat := false
	if l.peek() == '.' {
		isFloat = true
		l.advance()
		for isDigit(l.peek()) {
			l.advance()
		}
	}
	if c := l.peek(); c == 'e' || c == 'E' {
		isFloat = true
		l.advance()
		if c := l.peek(); c == '+' || c == '-' {
			l.advance()
		}
		if !isDigit(l.peek()) {
			l.bad(start, "malformed number %q: exponent has no digits", l.src[start:l.pos])
			return
		}
		for isDigit(l.peek()) {
			l.advance()
		}
	}
	// A number butting straight up against a letter (12abc) is a typo, not an
	// INT followed by an IDENT. Consume the tail so it surfaces as one error.
	if isAlpha(l.peek()) {
		for isAlpha(l.peek()) || isDigit(l.peek()) {
			l.advance()
		}
		l.bad(start, "malformed number %q", l.src[start:l.pos])
		return
	}
	kind := token.INT
	if isFloat {
		kind = token.FLOAT
	}
	l.add(kind, start, l.src[start:l.pos])
}

func (l *lexer) scanWord(start int) {
	for isAlpha(l.peek()) || isDigit(l.peek()) {
		l.advance()
	}
	word := l.src[start:l.pos]
	l.add(token.Lookup(word), start, word)
}

func (l *lexer) scanString(start int) {
	l.advance() // opening '
	var sb strings.Builder
	for {
		if l.atEnd() {
			l.bad(start, "unterminated string literal")
			return
		}
		c := l.advance()
		if c == '\'' {
			if l.peek() == '\'' {
				l.advance()
				sb.WriteByte('\'')
				continue
			}
			break
		}
		sb.WriteByte(c)
	}
	l.add(token.STRING, start, sb.String())
}

func (l *lexer) scanQuotedIdent(start int) {
	l.advance() // opening "
	var sb strings.Builder
	for {
		if l.atEnd() {
			l.bad(start, "unterminated quoted identifier")
			return
		}
		c := l.advance()
		if c == '"' {
			if l.peek() == '"' {
				l.advance()
				sb.WriteByte('"')
				continue
			}
			break
		}
		sb.WriteByte(c)
	}
	l.add(token.IDENT, start, sb.String())
}

func (l *lexer) scanSymbol(start int) {
	c := l.advance()
	switch c {
	case '(':
		l.add(token.LPAREN, start, "(")
	case ')':
		l.add(token.RPAREN, start, ")")
	case ',':
		l.add(token.COMMA, start, ",")
	case ';':
		l.add(token.SEMICOLON, start, ";")
	case '.':
		l.add(token.DOT, start, ".")
	case '+':
		l.add(token.PLUS, start, "+")
	case '-':
		l.add(token.MINUS, start, "-")
	case '*':
		l.add(token.STAR, start, "*")
	case '/':
		l.add(token.SLASH, start, "/")
	case '%':
		l.add(token.PERCENT, start, "%")
	case '=':
		l.add(token.EQ, start, "=")
	case '<':
		switch l.peek() {
		case '=':
			l.advance()
			l.add(token.LTE, start, "<=")
		case '>':
			l.advance()
			l.add(token.NEQ, start, "<>")
		default:
			l.add(token.LT, start, "<")
		}
	case '>':
		if l.peek() == '=' {
			l.advance()
			l.add(token.GTE, start, ">=")
		} else {
			l.add(token.GT, start, ">")
		}
	case '!':
		if l.peek() == '=' {
			l.advance()
			l.add(token.NEQ, start, "!=")
		} else {
			l.bad(start, "unexpected %q (did you mean '!='?)", string(c))
		}
	case '|':
		if l.peek() == '|' {
			l.advance()
			l.add(token.CONCAT, start, "||")
		} else {
			l.bad(start, "unexpected %q (did you mean '||'?)", string(c))
		}
	default:
		l.bad(start, "unexpected character %q", string(c))
	}
}

// skipTrivia consumes whitespace and comments (-- to end of line, /* */ block).
// It runs before every token, so a single '-' or '/' falls through here and is
// later lexed as the MINUS / SLASH operator.
func (l *lexer) skipTrivia() {
	for !l.atEnd() {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.advance()
		case c == '-' && l.peekNext() == '-':
			for !l.atEnd() && l.peek() != '\n' {
				l.advance()
			}
		case c == '/' && l.peekNext() == '*':
			l.advance()
			l.advance()
			for !l.atEnd() && !(l.peek() == '*' && l.peekNext() == '/') {
				l.advance()
			}
			if !l.atEnd() {
				l.advance() // *
				l.advance() // /
			}
		default:
			return
		}
	}
}

func (l *lexer) atEnd() bool { return l.pos >= len(l.src) }

func (l *lexer) peek() byte {
	if l.atEnd() {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) peekNext() byte {
	if l.pos+1 >= len(l.src) {
		return 0
	}
	return l.src[l.pos+1]
}

func (l *lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	return c
}

func (l *lexer) add(kind token.Kind, pos int, lexeme string) {
	l.tokens = append(l.tokens, token.Token{Kind: kind, Lexeme: lexeme, Pos: pos})
}

// bad records an error and emits an ILLEGAL token spanning start..pos, so the
// token stream stays aligned with the source and the parser can keep its place.
func (l *lexer) bad(start int, format string, args ...any) {
	l.errs = append(l.errs, &Error{Pos: start, Msg: fmt.Sprintf(format, args...)})
	l.add(token.ILLEGAL, start, l.src[start:l.pos])
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
