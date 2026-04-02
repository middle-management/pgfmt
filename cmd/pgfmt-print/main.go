package main

import (
	"fmt"
	"io"
	"os"

	"github.com/middle-management/pgfmt/printer"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
	}

	result, err := printer.FormatAugmented(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "format: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.WriteString(result)
}
