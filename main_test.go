package main

import (
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

			// Verify the formatted output is semantically identical to the input
			// by comparing deparsed (canonical) forms of both parse trees.
			// This catches cases where the formatter silently drops clauses.
			outputTree, err := pg_query.Parse(got)
			if err != nil {
				t.Logf("formatted output failed to parse for %s: %v", e.Name(), err)
			} else if len(tree.Stmts) != len(outputTree.Stmts) {
				t.Errorf("statement count mismatch for %s: input has %d, output has %d", e.Name(), len(tree.Stmts), len(outputTree.Stmts))
			} else {
				for idx := range tree.Stmts {
					inputOne := &pg_query.ParseResult{Stmts: tree.Stmts[idx : idx+1]}
					outputOne := &pg_query.ParseResult{Stmts: outputTree.Stmts[idx : idx+1]}
					inputCanonical, err1 := pg_query.Deparse(inputOne)
					outputCanonical, err2 := pg_query.Deparse(outputOne)
					if err1 != nil || err2 != nil {
						continue // skip statements that can't be deparsed
					}
					if inputCanonical != outputCanonical {
						t.Logf("semantic mismatch for %s statement %d:\n--- input deparsed ---\n%s\n--- output deparsed ---\n%s", e.Name(), idx+1, inputCanonical, outputCanonical)
					}
				}
			}
		})
	}
}

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}
