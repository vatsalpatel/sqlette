package repl

import (
	"strings"
	"testing"
)

func TestRunEcho(t *testing.T) {
	in := strings.NewReader("SELECT 1 + 1;\n.exit\n")
	var out strings.Builder

	code := Run(in, &out)
	assertEqual(t, 0, code)
	assertEqual(t, "SELECT 1 + 1\n", out.String())
}
