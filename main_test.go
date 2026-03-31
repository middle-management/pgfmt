package main

import (
	"os"
	"path/filepath"
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

func TestGoldenFixtures(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/fixtures/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixture files found")
	}

	for _, fixture := range fixtures {
		name := filepath.Base(fixture)
		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}

			input := strings.NewReader(string(inputBytes))
			output := &strings.Builder{}
			if err := print(input, output); err != nil {
				t.Fatal(err)
			}

			goldenPath := filepath.Join("testdata/fixtures/golden", name)
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden file %s: %v", goldenPath, err)
			}

			if output.String() != string(golden) {
				t.Errorf("output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, output.String(), string(golden))
			}
		})
	}
}
