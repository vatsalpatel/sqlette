package repl

import "strings"

// Scanner accumulates input lines into complete SQL statements terminated by a
// trailing ';'. It is intentionally naive: a ';' inside a string literal is not
// recognized — that becomes the lexer's job in M1.
type Scanner struct {
	buf strings.Builder
}

func (s *Scanner) Push(line string) (stmt string, ready bool) {
	if s.buf.Len() > 0 {
		s.buf.WriteByte('\n')
	}
	s.buf.WriteString(line)

	trimmed := strings.TrimRight(s.buf.String(), " \t\r\n")
	if !strings.HasSuffix(trimmed, ";") {
		return "", false
	}

	stmt = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	s.buf.Reset()
	return stmt, true
}

func (s *Scanner) Pending() bool {
	return s.buf.Len() > 0
}
