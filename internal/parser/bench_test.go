package parser_test

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/token"
)

type query struct {
	name string
	src  string
}

func corpus() []query {
	return []query{
		{"tiny", "SELECT 1"},
		{"select", "SELECT a, b FROM t WHERE x = 1 AND y > 2"},
		{"select_star", "SELECT * FROM users AS u WHERE u.age >= 30 AND u.name <> 'bob'"},
		{"update", "UPDATE t SET a = 1, b = 'x', c = a + 1 WHERE id = 42"},
		{"delete", "DELETE FROM t WHERE id = 42 AND flag IS NOT NULL"},
		{"explain", "EXPLAIN QUERY PLAN SELECT a, b FROM t WHERE x = 1"},
		{"begin", "BEGIN TRANSACTION"},
		{"deep_parens", "SELECT " + nest("a + b", 32)},
		{"and_chain", "SELECT x FROM t WHERE " + andChain(64)},
		{"insert_wide", insertRows(64)},
		{"create_wide", createCols(32)},
	}
}

func errorCorpus() []query {
	return []query{
		{"missing_table", "SELECT a FROM WHERE x = 1"},
		{"truncated_expr", "SELECT a FROM t WHERE x = "},
		{"trailing_junk", "SELECT a FROM t t2"},
	}
}

func nest(inner string, depth int) string {
	var sb strings.Builder
	sb.WriteString(strings.Repeat("(", depth))
	sb.WriteString(inner)
	sb.WriteString(strings.Repeat(")", depth))
	return sb.String()
}

func andChain(n int) string {
	var sb strings.Builder
	for i := range n {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		fmt.Fprintf(&sb, "c%d = %d", i, i)
	}
	return sb.String()
}

func insertRows(n int) string {
	var sb strings.Builder
	sb.WriteString("INSERT INTO t (a, b, c) VALUES ")
	for i := range n {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "(%d, 'r%d', %d.5)", i, i, i)
	}
	return sb.String()
}

func createCols(n int) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE t (")
	for i := range n {
		if i > 0 {
			sb.WriteString(", ")
		}
		switch i % 3 {
		case 0:
			fmt.Fprintf(&sb, "c%d INTEGER PRIMARY KEY", i)
		case 1:
			fmt.Fprintf(&sb, "c%d TEXT NOT NULL", i)
		default:
			fmt.Fprintf(&sb, "c%d REAL", i)
		}
	}
	sb.WriteString(")")
	return sb.String()
}

func mustLex(tb testing.TB, src string) []token.Token {
	tb.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		tb.Fatalf("lex %q: %v", src, err)
	}
	return toks
}

// BenchmarkLex, BenchmarkParse and BenchmarkLexParse are deliberately three
// benchmarks rather than one. A combined number cannot say which stage owns an
// allocation, and the two stages have completely different fixes.
func BenchmarkLex(b *testing.B) {
	for _, q := range corpus() {
		b.Run(q.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := lexer.Lex(q.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParse measures the parser alone: tokens are lexed once during setup
// so nothing in the timed loop belongs to the lexer.
func BenchmarkParse(b *testing.B) {
	for _, q := range corpus() {
		toks := mustLex(b, q.src)
		b.Run(q.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := parser.Parse(toks); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLexParse measures the whole front end, so lexer costs that the
// parser benchmark hides (keyword lookup, lexeme decoding) stay visible.
func BenchmarkLexParse(b *testing.B) {
	for _, q := range corpus() {
		b.Run(q.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				toks, err := lexer.Lex(q.src)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := parser.Parse(toks); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseError(b *testing.B) {
	for _, q := range errorCorpus() {
		toks := mustLex(b, q.src)
		b.Run(q.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := parser.Parse(toks); err == nil {
					b.Fatalf("%q: want a parse error, got none", q.src)
				}
			}
		})
	}
}

// BenchmarkParseRetained holds a fixed number of parsed statements alive, so
// each GC cycle has to trace them. BenchmarkParse only stresses allocation
// rate; this one also stresses mark cost, which is what a flatter or less
// pointer-heavy AST would actually improve.
func BenchmarkParseRetained(b *testing.B) {
	const live = 4096
	for _, q := range corpus() {
		toks := mustLex(b, q.src)
		b.Run(q.name, func(b *testing.B) {
			b.ReportAllocs()
			ring := make([]ast.Statement, live)
			for i := range ring {
				stmt, err := parser.Parse(toks)
				if err != nil {
					b.Fatal(err)
				}
				ring[i] = stmt
			}

			before := readGC()
			iters := 0
			b.ResetTimer()
			for b.Loop() {
				stmt, err := parser.Parse(toks)
				if err != nil {
					b.Fatal(err)
				}
				ring[iters%live] = stmt
				iters++
			}
			b.StopTimer()

			reportGC(b, before, iters)
			runtime.KeepAlive(ring)
		})
	}
}

type gcSnapshot struct {
	cycles uint64
	cpuSec float64
}

func readGC() gcSnapshot {
	s := []metrics.Sample{
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
	}
	metrics.Read(s)

	var snap gcSnapshot
	if s[0].Value.Kind() == metrics.KindUint64 {
		snap.cycles = s[0].Value.Uint64()
	}
	if s[1].Value.Kind() == metrics.KindFloat64 {
		snap.cpuSec = s[1].Value.Float64()
	}
	return snap
}

// gc-ns/op is CPU time summed across all Ps, not wall time, so on a parallel
// collector it can exceed the benchmark's own ns/op.
func reportGC(b *testing.B, before gcSnapshot, iters int) {
	b.Helper()
	if iters == 0 {
		return
	}
	after := readGC()
	b.ReportMetric(float64(after.cycles-before.cycles)/float64(iters), "gc-cycles/op")
	b.ReportMetric((after.cpuSec-before.cpuSec)*1e9/float64(iters), "gc-ns/op")
}
