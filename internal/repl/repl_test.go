package repl

import (
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

func runREPL(input string) string {
	in := strings.NewReader(input)
	var out strings.Builder
	Run(in, &out)
	return out.String()
}

func TestRunCreateInsertSelect(t *testing.T) {
	out := runREPL(
		"CREATE TABLE users (id INT, name TEXT);\n" +
			"INSERT INTO users VALUES (1, 'ada'), (2, 'alan');\n" +
			"SELECT * FROM users;\n" +
			".exit\n",
	)
	want := "ok\n" +
		"2 rows inserted\n" +
		"id | name\n" +
		"1 | 'ada'\n" +
		"2 | 'alan'\n"
	assert.Equal(t, want, out)
}

func TestRunWhere(t *testing.T) {
	out := runREPL(
		"CREATE TABLE users (id INT, name TEXT, age INT);\n" +
			"INSERT INTO users VALUES (1, 'ada', 36), (2, 'alan', 41);\n" +
			"SELECT name FROM users WHERE age > 40;\n" +
			".exit\n",
	)
	want := "ok\n" +
		"2 rows inserted\n" +
		"name\n" +
		"'alan'\n"
	assert.Equal(t, want, out)
}

func TestRunError(t *testing.T) {
	out := runREPL("SELECT * FROM missing;\n.exit\n")
	assert.Equal(t, "table missing does not exist\n", out)
}
