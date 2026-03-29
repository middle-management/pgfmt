package main

import (
	"fmt"
	"os"

	"github.com/middle-management/pgfmt/lsp"
)

func main() {
	s := lsp.NewServer(os.Stdin, os.Stdout)
	if err := s.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
