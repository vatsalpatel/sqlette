package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func projectEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.Open(dbPath(t))
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE t (a INT, b TEXT)")
	mustExec(t, eng, "INSERT INTO t VALUES (1, 'ada'), (2, 'alan')")
	mustExec(t, eng, "CREATE TABLE empty (a INT, b TEXT)")
	return eng
}

func TestSelectExpressions(t *testing.T) {
	eng := projectEngine(t)

	cases := []struct {
		name    string
		sql     string
		columns []string
		rows    [][]values.Value
	}{
		{
			"arithmetic",
			"SELECT a + 1 FROM t",
			[]string{"(+ a 1)"},
			[][]values.Value{{values.NewInteger(2)}, {values.NewInteger(3)}},
		},
		{
			"aliased arithmetic",
			"SELECT a * 2 AS double FROM t",
			[]string{"double"},
			[][]values.Value{{values.NewInteger(2)}, {values.NewInteger(4)}},
		},
		{
			"concat",
			"SELECT b || '!' FROM t WHERE a = 1",
			[]string{"(|| b '!')"},
			[][]values.Value{{values.NewText("ada!")}},
		},
		{
			"column beside expression",
			"SELECT b, a - 1 AS prev FROM t WHERE a = 2",
			[]string{"b", "prev"},
			[][]values.Value{{values.NewText("alan"), values.NewInteger(1)}},
		},
		{
			"comparison is a value",
			"SELECT a > 1 FROM t",
			[]string{"(> a 1)"},
			[][]values.Value{{values.NewInteger(0)}, {values.NewInteger(1)}},
		},
		{
			"null propagates",
			"SELECT a + NULL FROM t WHERE a = 1",
			[]string{"(+ a NULL)"},
			[][]values.Value{{values.NewNull()}},
		},
		{
			"through a table alias",
			"SELECT x.a + 1 AS next FROM t AS x WHERE x.a = 2",
			[]string{"next"},
			[][]values.Value{{values.NewInteger(3)}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := mustExec(t, eng, c.sql)
			assert.DeepEqual(t, c.columns, res.Columns)
			assert.DeepEqual(t, c.rows, res.Rows)
		})
	}
}

// A SELECT with no FROM reads one row of nothing, which is what makes an
// expression testable without a table to hang it off.
func TestSelectWithoutFrom(t *testing.T) {
	eng := projectEngine(t)

	res := mustExec(t, eng, "SELECT 1")
	assert.DeepEqual(t, []string{"1"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{{values.NewInteger(1)}}, res.Rows)

	res = mustExec(t, eng, "SELECT 1 + 1 AS two, 'x'")
	assert.DeepEqual(t, []string{"two", "'x'"}, res.Columns)
	assert.DeepEqual(t, [][]values.Value{{values.NewInteger(2), values.NewText("x")}}, res.Rows)
}

func TestSelectWithoutFromHonoursWhere(t *testing.T) {
	eng := projectEngine(t)

	res := mustExec(t, eng, "SELECT 1 WHERE 1 = 1")
	assert.Equal(t, 1, len(res.Rows))

	res = mustExec(t, eng, "SELECT 1 WHERE 1 = 0")
	assert.Equal(t, 0, len(res.Rows))
}

func TestSelectStarWithoutFrom(t *testing.T) {
	eng := projectEngine(t)

	_, err := tryExec(t, eng, "SELECT *")
	assert.ErrorContains(t, err, "no table")
}

// The behaviour change this stage makes deliberately: an unknown column is a
// plan-time error, so an empty table reports it instead of returning no rows
// and no complaint.
func TestUnknownColumnFailsOnAnEmptyTable(t *testing.T) {
	eng := projectEngine(t)

	_, err := tryExec(t, eng, "SELECT nosuch + 1 FROM empty")
	assert.ErrorContains(t, err, "nosuch")

	_, err = tryExec(t, eng, "SELECT a FROM empty WHERE nosuch = 1")
	assert.ErrorContains(t, err, "nosuch")

	_, err = tryExec(t, eng, "SELECT a FROM t WHERE nosuch = 1")
	assert.ErrorContains(t, err, "nosuch")
}

func TestExplainSelectWithoutFrom(t *testing.T) {
	eng := projectEngine(t)

	assert.Equal(t, "(project 1)\n  (onerow)", explain(t, eng, "SELECT 1"))
}
