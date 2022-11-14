package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v2"
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
			j, err := pg_query.ParseToJSON(string(buf))
			noError(t, err)
			tmp := map[string]interface{}{}
			err = json.Unmarshal([]byte(j), &tmp)
			noError(t, err)
			buf, err = json.MarshalIndent(tmp, "", "  ")
			noError(t, err)
			err = os.WriteFile("testdata/fixtures/output/"+e.Name()+".json", buf, 0o777)
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
