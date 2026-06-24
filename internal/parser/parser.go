package parser

import (
	"fmt"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/token"
)

type Error struct {
	Pos int
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("parse error at offset %d: %s", e.Pos, e.Msg)
}

type parser struct {
	toks []token.Token
	pos  int
}

// Parse turns a token slice from the lexer into a single statement AST.
func Parse(toks []token.Token) (ast.Statement, error) {
	p := &parser{toks: toks}

	stmt, err := p.parseStmt()
	if err != nil {
		return nil, err
	}

	p.accept(token.SEMICOLON)
	if !p.at(token.EOF) {
		got := p.peek()
		return nil, Error{Pos: got.Pos, Msg: fmt.Sprintf("unexpected %s after statement", got)}
	}
	return stmt, nil
}

func (p *parser) parseStmt() (ast.Statement, error) {
	peek := p.peek()
	switch peek.Kind {
	case token.SELECT:
		return p.parseSelect()
	default:
		return nil, Error{Pos: peek.Pos, Msg: fmt.Sprintf("expected a statement, got %s", peek)}
	}
}

// --- Statement parsing ---

func (p *parser) parseSelect() (ast.Statement, error) {
	p.advance() // skip SELECT
	stmt := &ast.SelectStmt{}
	cols := []ast.ResultColumn{}
	for {
		col, err := p.parseResultColumn()
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
		if !p.accept(token.COMMA) {
			break
		}
	}
	stmt.Columns = cols

	if p.accept(token.FROM) {
		tbl, err := p.expect(token.IDENT)
		if err != nil {
			return nil, err
		}
		stmt.From = ast.TableRef{Name: tbl.Lexeme}
		if p.accept(token.AS) {
			alias, err := p.expect(token.IDENT)
			if err != nil {
				return nil, err
			}
			stmt.From.Alias = alias.Lexeme
		}
	}

	if p.accept(token.WHERE) {
		where, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	return stmt, nil
}

func (p *parser) parseResultColumn() (ast.ResultColumn, error) {
	if p.accept(token.STAR) {
		return ast.ResultColumn{Expr: &ast.Star{}}, nil
	}
	expr, err := p.parseExpr(0)
	if err != nil {
		return ast.ResultColumn{}, err
	}
	col := ast.ResultColumn{Expr: expr}
	if p.accept(token.AS) {
		alias, err := p.expect(token.IDENT)
		if err != nil {
			return ast.ResultColumn{}, err
		}
		col.Alias = alias.Lexeme
	}
	return col, nil
}

func (p *parser) parseExpr(minBP int) (ast.Expression, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		tok := p.peek()
		bp := infixBP(tok.Kind)
		if bp <= minBP {
			return left, nil
		}
		p.advance()
		right, err := p.parseExpr(bp)
		if err != nil {
			return nil, err
		}
		left = &ast.Binary{Left: left, Op: tok.Kind, Right: right}
	}
}

func (p *parser) parsePrefix() (ast.Expression, error) {
	tok := p.peek()
	switch tok.Kind {
	case token.IDENT:
		p.advance()
		name := tok.Lexeme
		if p.accept(token.DOT) {
			if p.accept(token.STAR) {
				return &ast.Star{Table: name}, nil
			}
			member, err := p.expect(token.IDENT)
			if err != nil {
				return nil, err
			}
			return &ast.ColumnRef{Table: name, Name: member.Lexeme}, nil
		}
		return &ast.ColumnRef{Name: tok.Lexeme}, nil
	case token.INT, token.FLOAT, token.STRING, token.NULL:
		p.advance()
		return &ast.Literal{Value: tok.Lexeme, Kind: tok.Kind}, nil
	case token.LPAREN:
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(token.RPAREN); err != nil {
			return nil, err
		}
		return expr, nil
	case token.MINUS, token.NOT:
		p.advance()
		operand, err := p.parseExpr(prefixBP(tok.Kind))
		if err != nil {
			return nil, err
		}
		return &ast.Unary{Op: tok.Kind, Operand: operand}, nil
	default:
		return nil, Error{Pos: tok.Pos, Msg: fmt.Sprintf("expected an expression, got %s", tok)}
	}
}

func infixBP(tok token.Kind) int {
	switch tok {
	case token.OR:
		return 10
	case token.AND:
		return 20
	case token.EQ, token.NEQ, token.LT, token.LTE, token.GT, token.GTE:
		return 40
	case token.PLUS, token.MINUS:
		return 50
	case token.STAR, token.SLASH, token.PERCENT:
		return 60
	case token.CONCAT:
		return 70
	default:
		return 0
	}
}

func prefixBP(tok token.Kind) int {
	switch tok {
	case token.NOT:
		return 30
	case token.MINUS:
		return 100
	default:
		return 0
	}
}

// --- cursor helpers ---

func (p *parser) peek() token.Token {
	if p.pos >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos]
}

func (p *parser) advance() token.Token {
	t := p.peek()
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) at(k token.Kind) bool {
	return p.peek().Kind == k
}

func (p *parser) accept(k token.Kind) bool {
	if p.at(k) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) expect(k token.Kind) (token.Token, error) {
	if p.at(k) {
		return p.advance(), nil
	}
	got := p.peek()
	return got, Error{Pos: got.Pos, Msg: fmt.Sprintf("expected %s, got %s", k, got)}
}
