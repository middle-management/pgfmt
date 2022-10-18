package main

import (
	"os"
	"testing"
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
