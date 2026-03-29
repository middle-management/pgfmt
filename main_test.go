package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestPrintFixtures(t *testing.T) {
	fixtures, err := os.ReadDir("testdata/fixtures")
	noError(t, err)

	for _, e := range fixtures {
		if e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			i, err := os.Open("testdata/fixtures/" + e.Name())
			noError(t, err)

			buf, err := io.ReadAll(i)
			noError(t, err)

			tree, err := pg_query.Parse(string(buf))
			noError(t, err)

			buf, err = protojson.MarshalOptions{
				Indent: "  ",
			}.Marshal(tree)
			noError(t, err)

			err = os.WriteFile("testdata/fixtures/output/"+e.Name()+".parsed.json", buf, 0o777)
			noError(t, err)

			scan, err := pg_query.Scan(string(buf))
			noError(t, err)

			buf, err = protojson.MarshalOptions{
				Indent: "  ",
			}.Marshal(scan)
			noError(t, err)

			err = os.WriteFile("testdata/fixtures/output/"+e.Name()+".scanned.json", buf, 0o777)
			noError(t, err)

			_, err = i.Seek(0, 0)
			noError(t, err)

			o := &strings.Builder{}
			err = print(i, o)
			noError(t, err)

			got := o.String()

			// Write actual output for debugging
			err = os.WriteFile("testdata/fixtures/output/"+e.Name(), []byte(got), 0o777)
			noError(t, err)

			// Compare against golden file if it exists
			goldenPath := "testdata/fixtures/golden/" + e.Name()
			golden, err := os.ReadFile(goldenPath)
			if err == nil {
				if string(golden) != got {
					t.Errorf("output mismatch for %s\n--- golden ---\n%s\n--- got ---\n%s", e.Name(), string(golden), got)
				}
			}
		})
	}
}

// TestSchemaPreservation verifies that formatting SQL does not change its
// semantic meaning by comparing the parse trees of the original and formatted
// SQL (with source locations stripped).
func TestSchemaPreservation(t *testing.T) {
	fixtures, err := os.ReadDir("testdata/fixtures")
	noError(t, err)

	for _, e := range fixtures {
		if e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			raw, err := os.ReadFile("testdata/fixtures/" + e.Name())
			noError(t, err)

			original := string(raw)

			// Parse original SQL
			origTree, err := pg_query.Parse(original)
			noError(t, err)

			// Format the SQL
			o := &strings.Builder{}
			err = print(strings.NewReader(original), o)
			noError(t, err)
			formatted := o.String()

			// Parse formatted SQL
			fmtTree, err := pg_query.Parse(formatted)
			if err != nil {
				t.Fatalf("formatted SQL failed to parse: %v\nformatted output:\n%s", err, formatted)
			}

			// Serialize both trees to JSON and strip locations for comparison
			origJSON := treeToNormalizedJSON(t, origTree)
			fmtJSON := treeToNormalizedJSON(t, fmtTree)

			if origJSON != fmtJSON {
				t.Errorf("parse tree changed after formatting %s\n--- original tree ---\n%s\n--- formatted tree ---\n%s", e.Name(), origJSON, fmtJSON)
			}
		})
	}
}

// treeToNormalizedJSON serializes a parse tree to JSON and strips location
// fields so that only semantic content is compared.
func treeToNormalizedJSON(t *testing.T, tree *pg_query.ParseResult) string {
	t.Helper()

	b, err := protojson.MarshalOptions{Indent: "  "}.Marshal(tree)
	if err != nil {
		t.Fatalf("failed to marshal parse tree: %v", err)
	}

	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	stripLocations(data)

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("failed to re-marshal JSON: %v", err)
	}
	return string(out)
}

// stripLocations recursively removes "location", "stmt_location", and
// "stmt_len" fields from a JSON structure, since these are source positions
// that change with formatting.
func stripLocations(v any) {
	switch val := v.(type) {
	case map[string]any:
		delete(val, "location")
		delete(val, "stmtLocation")
		delete(val, "stmtLen")
		delete(val, "stmt_location")
		delete(val, "stmt_len")
		for _, child := range val {
			stripLocations(child)
		}
	case []any:
		for _, item := range val {
			stripLocations(item)
		}
	}
}

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}
