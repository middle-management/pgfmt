package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v2"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	result, err := pg_query.Parse(string(input))
	if err != nil {
		return err
	}

	for _, stmt := range result.Stmts {
		output := &printer.Printer{
			Builder: &strings.Builder{},
		}
		output.Print(stmt.Stmt)
		fmt.Println(output.String())
	}

	return nil
}
