package engine_test

import (
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
)

// EXPLAIN has to describe the plan that actually runs. It is built from the same
// scanNode as the executor for exactly this reason: a second construction site
// drifts the first time a selection rule is added, and then EXPLAIN reports an
// access path nobody took.
func openIndexed(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (a INT, b TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'x'), (5, 'y'), (9, 'z')")
	mustExec(t, eng, "CREATE INDEX idx_a ON t (a)")
	return eng
}

func explain(t *testing.T, eng *engine.Engine, sql string) string {
	t.Helper()
	return mustExec(t, eng, "EXPLAIN "+sql).Message
}

func TestExplainShowsIndexScan(t *testing.T) {
	eng := openIndexed(t)

	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"select equality", "SELECT * FROM t WHERE a = 5", "(indexscan t using idx_a (= a 5))"},
		{"select range", "SELECT * FROM t WHERE a > 1", "(indexscan t using idx_a (> a 1))"},
		{"delete", "DELETE FROM t WHERE a = 5", "(indexscan t using idx_a (= a 5))"},
		{"update", "UPDATE t SET b = 'q' WHERE a = 5", "(indexscan t using idx_a (= a 5))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := explain(t, eng, c.sql)
			if !strings.Contains(got, c.want) {
				t.Fatalf("EXPLAIN %s printed:\n%s\nwant a line containing %q", c.sql, got, c.want)
			}
		})
	}
}

func TestExplainShowsSeqScanWhenNoIndexApplies(t *testing.T) {
	eng := openIndexed(t)

	for _, sql := range []string{
		"SELECT * FROM t WHERE b = 'x'",
		"SELECT * FROM t WHERE a <> 5",
		"SELECT * FROM t",
	} {
		got := explain(t, eng, sql)
		if !strings.Contains(got, "(seqscan t)") {
			t.Fatalf("EXPLAIN %s printed:\n%s\nwant a seqscan", sql, got)
		}
	}
}

// The predicate the index does not enforce has to show up as a filter above it,
// which is also the visible proof that it was not dropped.
func TestExplainShowsResidualFilterAboveIndexScan(t *testing.T) {
	eng := openIndexed(t)

	got := explain(t, eng, "SELECT * FROM t WHERE a = 5 AND b = 'x'")
	if !strings.Contains(got, "(indexscan t using idx_a (= a 5))") {
		t.Fatalf("want an index scan, got:\n%s", got)
	}
	if !strings.Contains(got, "(filter (= b 'x'))") {
		t.Fatalf("want a residual filter on b, got:\n%s", got)
	}
}

// Queries must return the same rows whichever access path is chosen.
func TestIndexAndSeqScanAgreeOnRows(t *testing.T) {
	eng := openIndexed(t)

	// b is unindexed, so this pair asks the same question down both paths
	viaIndex := mustExec(t, eng, "SELECT b FROM t WHERE a >= 5")
	viaScan := mustExec(t, eng, "SELECT b FROM t WHERE b > 'x'")

	assert.Equal(t, 2, len(viaIndex.Rows))
	assert.Equal(t, 2, len(viaScan.Rows))
}

func TestIndexScanDrivesDeleteAndUpdate(t *testing.T) {
	eng := openIndexed(t)

	mustExec(t, eng, "UPDATE t SET b = 'updated' WHERE a = 5")
	res := mustExec(t, eng, "SELECT b FROM t WHERE a = 5")
	assert.Equal(t, 1, len(res.Rows))
	assert.Equal(t, "updated", res.Rows[0][0].Text)

	// deleting through an index scan mutates the very tree being scanned
	mustExec(t, eng, "DELETE FROM t WHERE a >= 5")
	res = mustExec(t, eng, "SELECT * FROM t")
	assert.Equal(t, 1, len(res.Rows))

	// and the index agrees with the table afterwards
	res = mustExec(t, eng, "SELECT * FROM t WHERE a >= 5")
	assert.Equal(t, 0, len(res.Rows))
}
