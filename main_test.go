package main

import (
	"io"
	"os"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v5"
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

			o, err := os.Create("testdata/fixtures/output/" + e.Name())
			noError(t, err)

			err = print(i, o)
			noError(t, err)
		})
	}
}

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}
