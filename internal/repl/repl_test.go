package repl

import (
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

func TestRunParse(t *testing.T) {
	in := strings.NewReader("SELECT 1 + 1;\n.exit\n")
	var out strings.Builder

	code := Run(in, &out)
	assert.Equal(t, 0, code)
	assert.Equal(t, "(select (cols (+ 1 1)))\n", out.String())
}
