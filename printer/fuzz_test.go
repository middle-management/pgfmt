//go:build !js && !wasip1

package printer

import (
	"strings"
	"testing"
)

// FuzzFormatIdempotent feeds arbitrary inputs to Format and asserts that any
// successfully-formatted output is stable on a second pass. This catches
// printer instabilities where Format(Format(x)) drifts away from Format(x).
//
// Run with: go test -run=^$ -fuzz=FuzzFormatIdempotent ./printer
func FuzzFormatIdempotent(f *testing.F) {
	seeds := []string{
		"select 1",
		"SELECT id, name FROM users WHERE id = 1",
		"WITH t AS (SELECT 1) SELECT * FROM t",
		"INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING",
		"UPDATE t SET x = 1 WHERE id = 2 RETURNING *",
		"CREATE TABLE t (id int PRIMARY KEY, name text NOT NULL)",
		"-- a comment\nSELECT 1;\n-- trailing\n",
		"SELECT * FROM a JOIN b ON a.id = b.id",
		"SELECT (1 + 2) * 3",
		"SELECT array[1,2,3][1]",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Skip inputs the parser rejects — we only care about valid SQL.
		first, err := Format(input)
		if err != nil {
			t.Skip()
		}
		// Skip pathological inputs whose formatted form is empty.
		if strings.TrimSpace(first) == "" {
			t.Skip()
		}
		second, err := Format(first)
		if err != nil {
			t.Fatalf("re-format of formatted output failed: %v\n--- formatted ---\n%s", err, first)
		}
		if first != second {
			t.Fatalf("Format is not idempotent\n--- input ---\n%q\n--- first pass ---\n%s\n--- second pass ---\n%s",
				input, first, second)
		}
	})
}
