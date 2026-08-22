package engine_test

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
)

type modelRow struct {
	a int64
	b string
}

// An end-to-end consistency fuzz driven entirely through SQL. Stage D's storage
// fuzz recomputes each index from a table scan; this one never looks inside,
// and instead keeps a model of what the table should hold and checks that
// index-driven queries agree with it after every statement.
//
// The whole run sits inside one transaction. That is not about durability: an
// autocommit statement costs two fsyncs, so a few hundred of them would take
// twenty seconds to test something that has nothing to do with commit.
func TestIndexedQueriesTrackAModelUnderRandomMutations(t *testing.T) {
	const ops = 400

	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	defer eng.Close()

	mustExec(t, eng, "CREATE TABLE t (a INT, b TEXT)")
	mustExec(t, eng, "CREATE INDEX idx_a ON t (a)")
	mustExec(t, eng, "BEGIN")

	var model []modelRow

	// a small key space so values collide constantly and the rowid suffix is
	// always doing work
	const keys = 8
	r := rand.New(rand.NewSource(11))

	bFor := func(k int64, n int) string { return fmt.Sprintf("v%d-%d", k, n) }

	for op := range ops {
		k := int64(r.Intn(keys))
		switch {
		case len(model) == 0 || r.Intn(100) < 50:
			b := bFor(k, op)
			mustExec(t, eng, fmt.Sprintf("INSERT INTO t VALUES (%d, '%s')", k, b))
			model = append(model, modelRow{k, b})
		case r.Intn(100) < 60:
			b := bFor(k, op)
			mustExec(t, eng, fmt.Sprintf("UPDATE t SET b = '%s' WHERE a = %d", b, k))
			for i := range model {
				if model[i].a == k {
					model[i].b = b
				}
			}
		default:
			mustExec(t, eng, fmt.Sprintf("DELETE FROM t WHERE a = %d", k))
			model = slices.DeleteFunc(model, func(x modelRow) bool { return x.a == k })
		}

		// the point lookup that an index serves, checked against the model
		assertMatches(t, eng, model, k)
		// and a range, so the walk-and-stop path is exercised too
		assertRangeMatches(t, eng, model, keys/2)

		if len(mustExec(t, eng, "SELECT * FROM t").Rows) != len(model) {
			t.Fatalf("op %d: table holds %d rows, model says %d",
				op, len(mustExec(t, eng, "SELECT * FROM t").Rows), len(model))
		}
	}

	mustExec(t, eng, "COMMIT")

	// and it all survives the commit
	assertMatches(t, eng, model, 0)
	assert.Equal(t, len(model), len(mustExec(t, eng, "SELECT * FROM t").Rows))
}

func assertMatches(t *testing.T, eng *engine.Engine, model []modelRow, k int64) {
	t.Helper()

	var want []string
	for _, m := range model {
		if m.a == k {
			want = append(want, m.b)
		}
	}
	got := selectB(t, eng, fmt.Sprintf("SELECT b FROM t WHERE a = %d", k))

	slices.Sort(want)
	if !slices.Equal(want, got) {
		t.Fatalf("SELECT b WHERE a = %d returned %v, model says %v", k, got, want)
	}
}

func assertRangeMatches(t *testing.T, eng *engine.Engine, model []modelRow, pivot int) {
	t.Helper()

	var want []string
	for _, m := range model {
		if m.a >= int64(pivot) {
			want = append(want, m.b)
		}
	}
	got := selectB(t, eng, fmt.Sprintf("SELECT b FROM t WHERE a >= %d", pivot))

	slices.Sort(want)
	if !slices.Equal(want, got) {
		t.Fatalf("SELECT b WHERE a >= %d returned %v, model says %v", pivot, got, want)
	}
}

func selectB(t *testing.T, eng *engine.Engine, sql string) []string {
	t.Helper()
	res := mustExec(t, eng, sql)
	out := make([]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, row[0].Text)
	}
	slices.Sort(out)
	return out
}
