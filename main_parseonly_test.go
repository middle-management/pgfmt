package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/middle-management/pgfmt/printer"
)

func TestParseOnly(t *testing.T) {
	input := strings.NewReader("SELECT 1;")
	var out bytes.Buffer
	err := parseOnly(input, &out)
	if err != nil {
		t.Fatal(err)
	}

	var ast printer.AugmentedAST
	if err := json.Unmarshal(out.Bytes(), &ast); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out.String())
	}
	if len(ast.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(ast.Stmts))
	}
}
