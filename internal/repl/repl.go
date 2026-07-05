package repl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vatsalpatel/sqlette/internal/engine"
	"github.com/vatsalpatel/sqlette/internal/lexer"
	"github.com/vatsalpatel/sqlette/internal/parser"
)

const (
	promptNew  = "sqlette> "
	promptCont = "   ...> "
)

// Run reads statements from in until EOF, parsing each completed statement and
// printing its AST to out. Dot-commands such as .exit are handled inline.
// Prompts are shown only when in is an interactive terminal. It returns a
// process exit code.
func Run(in io.Reader, out io.Writer) int {
	reader := bufio.NewReader(in)
	interactive := isTerminal(in)
	var scan Scanner
	eng, err := engine.New()
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}

	prompt(out, &scan, interactive)
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		if line != "" || err == nil {
			if !scan.Pending() && isQuit(line) {
				return 0
			}
			if stmt, ready := scan.Push(line); ready && stmt != "" {
				execute(out, eng, stmt)
			}
		}

		if err != nil {
			if scan.Pending() {
				fmt.Fprintln(out)
			}
			return 0
		}
		prompt(out, &scan, interactive)
	}
}

// create table users (id int, name text);
// insert into users values (1, 'ada'), (2, 'alan'), (3, 'grace');
// select * from users;
func execute(out io.Writer, eng *engine.Engine, query string) {
	tokens, err := lexer.Lex(query)
	if err != nil {
		fmt.Fprintln(out, err)
		return
	}
	stmt, err := parser.Parse(tokens)
	if err != nil {
		fmt.Fprintln(out, err)
		return
	}
	res, err := eng.Exec(stmt)
	if err != nil {
		fmt.Fprintln(out, err)
		return
	}
	formatResult(out, res)
}

func formatResult(out io.Writer, res *engine.Result) {
	if len(res.Rows) == 0 {
		if res.Message != "" {
			fmt.Fprintln(out, res.Message)
		}
		return
	}
	fmt.Fprintln(out, strings.Join(res.Columns, " | "))
	for _, r := range res.Rows {
		row := make([]string, len(r))
		for i, v := range r {
			row[i] = v.String()
		}
		fmt.Fprintln(out, strings.Join(row, " | "))
	}

}

func prompt(out io.Writer, scan *Scanner, interactive bool) {
	if !interactive {
		return
	}
	if scan.Pending() {
		fmt.Fprint(out, promptCont)
	} else {
		fmt.Fprint(out, promptNew)
	}
}

func isQuit(line string) bool {
	switch strings.TrimSpace(line) {
	case ".exit", ".quit", ".q":
		return true
	}
	return false
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
