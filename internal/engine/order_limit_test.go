package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// t: (3,'ada') (1,'linus') (2,'grace') (NULL,'bob')
func sortEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (a INT, b TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (3, 'ada'), (1, 'linus'), (2, 'grace'), (NULL, 'bob')")
	return eng
}

func texts(res *engine.Result) []string {
	out := []string{}
	for _, row := range res.Rows {
		out = append(out, row[0].Text)
	}
	return out
}

func TestOrderByEndToEnd(t *testing.T) {
	eng := sortEngine(t)

	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"ascending", "SELECT b FROM t ORDER BY a", []string{"bob", "linus", "grace", "ada"}},
		{"descending", "SELECT b FROM t ORDER BY a DESC", []string{"ada", "grace", "linus", "bob"}},
		{"by text", "SELECT b FROM t ORDER BY b", []string{"ada", "bob", "grace", "linus"}},
		{"unselected column", "SELECT b FROM t WHERE a > 0 ORDER BY a DESC", []string{"ada", "grace", "linus"}},
		{"expression", "SELECT b FROM t WHERE a > 0 ORDER BY 0 - a", []string{"ada", "grace", "linus"}},
		{"ordinal", "SELECT b, a FROM t ORDER BY 2 DESC", []string{"ada", "grace", "linus", "bob"}},
		{"alias", "SELECT b, 0 - a AS neg FROM t WHERE a > 0 ORDER BY neg", []string{"ada", "grace", "linus"}},
		{"two keys", "SELECT b FROM t ORDER BY a IS NULL, b DESC", []string{"linus", "grace", "ada", "bob"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.DeepEqual(t, c.want, texts(mustExec(t, eng, c.sql)))
		})
	}
}

// LIMIT counts rows of the finished result, so with an ORDER BY it returns the
// top of the sorted order rather than an arbitrary slice that is then sorted.
func TestLimitEndToEnd(t *testing.T) {
	eng := sortEngine(t)

	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"top two", "SELECT b FROM t ORDER BY a DESC LIMIT 2", []string{"ada", "grace"}},
		{"offset into the order", "SELECT b FROM t ORDER BY a DESC LIMIT 2 OFFSET 1", []string{"grace", "linus"}},
		{"zero", "SELECT b FROM t ORDER BY a LIMIT 0", nil},
		{"past the end", "SELECT b FROM t ORDER BY a LIMIT 5 OFFSET 99", nil},
		{"more than there are", "SELECT b FROM t ORDER BY a DESC LIMIT 99", []string{"ada", "grace", "linus", "bob"}},
		{"unlimited", "SELECT b FROM t ORDER BY a DESC LIMIT -1", []string{"ada", "grace", "linus", "bob"}},
		{"without an order", "SELECT b FROM t LIMIT 2", []string{"ada", "linus"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := texts(mustExec(t, eng, c.sql))
			if len(c.want) == 0 {
				assert.Equal(t, 0, len(got))
				return
			}
			assert.DeepEqual(t, c.want, got)
		})
	}
}

func TestOrderByWithoutFrom(t *testing.T) {
	eng := sortEngine(t)

	res := mustExec(t, eng, "SELECT 1 ORDER BY 1 LIMIT 1")
	assert.DeepEqual(t, [][]values.Value{{values.NewInteger(1)}}, res.Rows)
}

// An index changes the access path underneath the sort and must not change the
// answer above it.
func TestOrderByAgreesWithAndWithoutAnIndex(t *testing.T) {
	eng := sortEngine(t)

	before := texts(mustExec(t, eng, "SELECT b FROM t WHERE a > 0 ORDER BY a DESC"))
	mustExec(t, eng, "CREATE INDEX idx_a ON t (a)")
	after := texts(mustExec(t, eng, "SELECT b FROM t WHERE a > 0 ORDER BY a DESC"))

	assert.DeepEqual(t, before, after)
	assert.DeepEqual(t, []string{"ada", "grace", "linus"}, after)
}

func TestExplainShowsTheWholePipeline(t *testing.T) {
	eng := sortEngine(t)

	want := "(limit 2 offset 1)\n  (project b)\n    (sort (desc a))\n      (filter (> a 0))\n        (seqscan t)"
	assert.Equal(t, want, explain(t, eng, "SELECT b FROM t WHERE a > 0 ORDER BY a DESC LIMIT 2 OFFSET 1"))
}
