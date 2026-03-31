//go:build !js

package main

import (
	"fmt"
	"os"

	"github.com/middle-management/pgfmt/lsp"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(version)
		return
	}

	s := lsp.NewServer(os.Stdin, os.Stdout)
	if err := s.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
