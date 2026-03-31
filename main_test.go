package main

import (
	"strings"
	"testing"
)

func TestPrint(t *testing.T) {
	input := strings.NewReader("select 1;")
	output := &strings.Builder{}
	err := print(input, output)
	if err != nil {
		t.Fatal(err)
	}
	expected := "SELECT\n\t1;\n\n"
	if output.String() != expected {
		t.Errorf("got %q, want %q", output.String(), expected)
	}
}
