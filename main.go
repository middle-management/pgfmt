package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v5"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	return print(os.Stdin, os.Stdout)
}

func print(input io.Reader, output io.Writer) error {
	buffer, err := io.ReadAll(input)
	if err != nil {
		return err
	}

	result, err := pg_query.Parse(string(buffer))
	if err != nil {
		return err
	}

	for _, stmt := range result.Stmts {
		b := &strings.Builder{}
		p := &printer.Printer{Builder: b}
		p.Print(stmt.Stmt)
		_, err := io.WriteString(output, b.String()+";\n\n")
		if err != nil {
			return err
		}
	}

	return nil
}
