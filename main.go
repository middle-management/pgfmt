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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--parse-only" {
		return parseOnly(os.Stdin, os.Stdout)
	}
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

func parseOnly(input io.Reader, output io.Writer) error {
	buffer, err := io.ReadAll(input)
	if err != nil {
		return err
	}

	augmented, err := printer.Augment(string(buffer))
	if err != nil {
		return err
	}

	_, err = output.Write(augmented)
	if err != nil {
		return err
	}
	_, err = io.WriteString(output, "\n")
	return err
}
