package parser_test

import (
	"reflect"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
)

// FuzzParse is a guardrail for allocation work on the parser: parsing must not
// panic, and parsing the same tokens twice must build identical trees. The
// second check is what catches an arena or node-pool that hands out memory
// still reachable from an earlier parse.
func FuzzParse(f *testing.F) {
	for _, q := range corpus() {
		f.Add(q.src)
	}
	for _, q := range errorCorpus() {
		f.Add(q.src)
	}

	f.Fuzz(func(t *testing.T, src string) {
		toks, err := lexer.Lex(src)
		if err != nil {
			return
		}

		first, err := parser.Parse(toks)
		if err != nil {
			return
		}
		second, err := parser.Parse(toks)
		if err != nil {
			t.Fatalf("%q parsed once but failed on reparse: %v", src, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("%q: reparse differs\nfirst:  %#v\nsecond: %#v", src, first, second)
		}
	})
}
