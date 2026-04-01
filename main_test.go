package main

import (
	"compress/gzip"
	"io"
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

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	if strings.HasSuffix(path, ".gz") {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		r, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGoldenFixtures(t *testing.T) {
	sqlFiles, _ := filepath.Glob("testdata/fixtures/*.sql")
	gzFiles, _ := filepath.Glob("testdata/fixtures/*.sql.gz")
	fixtures := append(sqlFiles, gzFiles...)
	if len(fixtures) == 0 {
		t.Fatal("no fixture files found")
	}

	for _, fixture := range fixtures {
		name := filepath.Base(fixture)
		// Display name without .gz for readability
		testName := strings.TrimSuffix(name, ".gz")
		t.Run(testName, func(t *testing.T) {
			inputBytes := readFixture(t, fixture)

			input := strings.NewReader(string(inputBytes))
			output := &strings.Builder{}
			if err := print(input, output); err != nil {
				t.Fatal(err)
			}

			// Try .gz golden first, fall back to plain
			goldenName := strings.TrimSuffix(name, ".gz")
			goldenPath := filepath.Join("testdata/fixtures/golden", goldenName+".gz")
			if _, err := os.Stat(goldenPath); err != nil {
				goldenPath = filepath.Join("testdata/fixtures/golden", goldenName)
			}
			golden := readFixture(t, goldenPath)

			if output.String() != string(golden) {
				// For large files, don't dump the full output
				if len(output.String()) > 1000 {
					t.Errorf("output does not match golden file %s (got %d bytes, want %d bytes)", goldenPath, len(output.String()), len(golden))
				} else {
					t.Errorf("output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, output.String(), string(golden))
				}
			}
		})
	}
}
