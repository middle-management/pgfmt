package main

import (
	"fmt"
	"io"
	"os"

	"github.com/middle-management/pgfmt/printer"
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

	formatted, err := printer.Format(string(buffer))
	if err != nil {
		return err
	}

	_, err = io.WriteString(output, formatted)
	return err
}
