package engine_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
	"github.com/vatsalpatel/sqlette/internal/values"

	_ "modernc.org/sqlite"
)

// Differential testing against real SQLite, promised at M5 and landing here
// because indexes are what make it worth the dependency: an index bug shows up
// as fewer or extra rows, which is exactly what a row-by-row diff catches and
// what a unit test written against your own mental model does not.
//
// Both sides are canonicalised from *values*, never from printed output, so
// float formatting and quoting can never be the thing that differs.

// canonicalise renders one result set. Rows are sorted because neither engine
// promises an order without ORDER BY, and sqlette deliberately returns index
// order when it uses an index while SQLite may do anything at all.
func canonicalise(rows []string) string {
	out := slices.Clone(rows)
	slices.Sort(out)
	return strings.Join(out, "\n")
}

func fmtValue(v values.Value) string {
	switch v.Type {
	case values.Null:
		return "NULL"
	case values.Integer:
		return strconv.FormatInt(v.Int, 10)
	case values.Real:
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case values.Text:
		return v.Text
	default:
		return fmt.Sprintf("x'%x'", v.Blob)
	}
}

func fmtScanned(a any) string {
	switch x := a.(type) {
	case nil:
		return "NULL"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	case []byte:
		return fmt.Sprintf("x'%x'", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func isQuery(stmt string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "SELECT")
}

// statements splits a script on semicolons. The scripts deliberately contain no
// semicolons inside string literals, which keeps this honest.
func statements(script string) []string {
	var out []string
	for _, s := range strings.Split(script, ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func runSqlette(t *testing.T, script string) []string {
	t.Helper()
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()

	var results []string
	for _, stmt := range statements(script) {
		toks, err := lexer.Lex(stmt)
		if err != nil {
			t.Fatalf("sqlette lex %q: %v", stmt, err)
		}
		parsed, err := parser.Parse(toks)
		if err != nil {
			t.Fatalf("sqlette parse %q: %v", stmt, err)
		}
		res, err := eng.Exec(parsed)
		if err != nil {
			t.Fatalf("sqlette exec %q: %v", stmt, err)
		}
		if !isQuery(stmt) {
			continue
		}
		var rows []string
		for _, row := range res.Rows {
			cells := make([]string, len(row))
			for i, v := range row {
				cells[i] = fmtValue(v)
			}
			rows = append(rows, strings.Join(cells, "|"))
		}
		results = append(results, canonicalise(rows))
	}
	return results
}

func runSQLite(t *testing.T, script string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ref.db"))
	assert.NoError(t, err)
	defer db.Close()

	var results []string
	for _, stmt := range statements(script) {
		if !isQuery(stmt) {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("sqlite exec %q: %v", stmt, err)
			}
			continue
		}
		q, err := db.Query(stmt)
		if err != nil {
			t.Fatalf("sqlite query %q: %v", stmt, err)
		}
		cols, err := q.Columns()
		assert.NoError(t, err)

		var rows []string
		for q.Next() {
			cells := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range cells {
				ptrs[i] = &cells[i]
			}
			assert.NoError(t, q.Scan(ptrs...))
			rendered := make([]string, len(cells))
			for i, c := range cells {
				rendered[i] = fmtScanned(c)
			}
			rows = append(rows, strings.Join(rendered, "|"))
		}
		assert.NoError(t, q.Err())
		assert.NoError(t, q.Close())
		results = append(results, canonicalise(rows))
	}
	return results
}

func TestDifferentialAgainstSQLite(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("testdata", "diff", "*.sql"))
	assert.NoError(t, err)
	if len(scripts) == 0 {
		t.Fatal("no differential scripts found under testdata/diff")
	}

	for _, path := range scripts {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			assert.NoError(t, err)
			script := string(raw)

			mine := runSqlette(t, script)
			theirs := runSQLite(t, script)

			queries := 0
			for _, stmt := range statements(script) {
				if isQuery(stmt) {
					queries++
				}
			}
			if len(mine) != queries || len(theirs) != queries {
				t.Fatalf("collected %d sqlette and %d sqlite result sets, want %d", len(mine), len(theirs), queries)
			}

			i := 0
			for _, stmt := range statements(script) {
				if !isQuery(stmt) {
					continue
				}
				if mine[i] != theirs[i] {
					t.Errorf("%s\n  sqlette:\n%s\n  sqlite:\n%s",
						stmt, indentBlock(mine[i]), indentBlock(theirs[i]))
				}
				i++
			}
		})
	}
}

func indentBlock(s string) string {
	if s == "" {
		return "    (no rows)"
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
