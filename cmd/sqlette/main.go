package main

import (
	"os"

	"github.com/vatsalpatel/sqlette/internal/repl"
)

func main() {
	os.Exit(repl.Run(os.Stdin, os.Stdout))
}
