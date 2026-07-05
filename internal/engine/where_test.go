package engine_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/values"
)

// users: (1,ada,36) (2,alan,41) (3,grace,28) (4,bob,NULL)
func usersEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.New()
	assert.NoError(t, err)
	mustExec(t, eng, "CREATE TABLE users (id INT, name TEXT, age INT)")
	mustExec(t, eng, "INSERT INTO users VALUES (1, 'ada', 36)")
	mustExec(t, eng, "INSERT INTO users VALUES (2, 'alan', 41)")
	mustExec(t, eng, "INSERT INTO users VALUES (3, 'grace', 28)")
	mustExec(t, eng, "INSERT INTO users VALUES (4, 'bob', NULL)")
	return eng
}

func names(res *engine.Result) []string {
	out := []string{}
	for _, row := range res.Rows {
		out = append(out, row[0].Text)
	}
	return out
}

func TestWhere(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"gt", "SELECT name FROM users WHERE age > 30", []string{"ada", "alan"}},
		{"lt drops null", "SELECT name FROM users WHERE age < 30", []string{"grace"}},
		{"eq", "SELECT name FROM users WHERE age = 41", []string{"alan"}},
		{"neq drops null", "SELECT name FROM users WHERE age <> 36", []string{"alan", "grace"}},
		{"gte", "SELECT name FROM users WHERE age >= 36", []string{"ada", "alan"}},
		{"lte drops null", "SELECT name FROM users WHERE age <= 28", []string{"grace"}},
		{"text eq", "SELECT name FROM users WHERE name = 'ada'", []string{"ada"}},
		{"and", "SELECT name FROM users WHERE age > 30 AND id < 2", []string{"ada"}},
		{"or", "SELECT name FROM users WHERE age < 30 OR age > 40", []string{"alan", "grace"}},
		{"not", "SELECT name FROM users WHERE NOT age < 30", []string{"ada", "alan"}},
		{"null comparison drops row", "SELECT name FROM users WHERE age < 100", []string{"ada", "alan", "grace"}},
		{"or null true-dominates", "SELECT name FROM users WHERE age > 100 OR name = 'bob'", []string{"bob"}},
		{"and null drops", "SELECT name FROM users WHERE age > 100 AND name = 'bob'", []string{}},
		{"matches nothing", "SELECT name FROM users WHERE age > 100", []string{}},
		{"matches all", "SELECT name FROM users WHERE id > 0", []string{"ada", "alan", "grace", "bob"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := usersEngine(t)
			res := mustExec(t, eng, tt.sql)
			assert.DeepEqual(t, tt.want, names(res))
		})
	}
}

func TestWhereIsNull(t *testing.T) {
	eng := usersEngine(t)
	res := mustExec(t, eng, "SELECT name FROM users WHERE age IS NULL")
	assert.DeepEqual(t, []string{"bob"}, names(res))
}

func TestWhereIsNotNull(t *testing.T) {
	eng := usersEngine(t)
	res := mustExec(t, eng, "SELECT name FROM users WHERE age IS NOT NULL")
	assert.DeepEqual(t, []string{"ada", "alan", "grace"}, names(res))
}

func TestSelectStarWithNull(t *testing.T) {
	eng := usersEngine(t)
	res := mustExec(t, eng, "SELECT * FROM users WHERE id = 4")
	assert.DeepEqual(t, [][]values.Value{
		{values.NewInteger(4), values.NewText("bob"), values.NewNull()},
	}, res.Rows)
}
