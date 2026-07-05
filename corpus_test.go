package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// TestPostgresRegressionCorpus runs the formatter over the PostgreSQL
// regression test suite (src/test/regress/sql and src/pl/plpgsql/src/sql)
// and classifies every statement:
//
//	panic           the printer panicked
//	format-error    printer.Format returned an error
//	output-invalid  the formatted output no longer parses
//	roundtrip-diff  deparse(parse(input)) != deparse(parse(output)) after
//	                normalization (see idempotency_test.go)
//	not-idempotent  formatting the output again changed it
//
// Statements that don't parse standalone (intentional syntax errors, inline
// COPY data, psql-isms) are skipped — they say nothing about the formatter.
//
// Results are compared against testdata/corpus_baseline.txt. New failures
// fail the test; fixes must update the baseline (like golden files):
//
//	PGFMT_UPDATE_BASELINE=1 PGFMT_CORPUS=1 go test -run TestPostgresRegressionCorpus
//
// The corpus version is pinned to the PostgreSQL release matching the
// libpg_query version in go.mod, so statement classification is stable.
// The tarball is downloaded once into testdata/corpus/ (gitignored).
//
// The test is skipped unless PGFMT_CORPUS=1 is set: it downloads ~28MB on
// first run and takes a few minutes.
func TestPostgresRegressionCorpus(t *testing.T) {
	if os.Getenv("PGFMT_CORPUS") == "" {
		t.Skip("set PGFMT_CORPUS=1 to run the PostgreSQL regression corpus test")
	}

	corpusDir := ensureCorpus(t)

	type fileResult struct {
		name   string
		counts map[string]int
	}
	var files []string
	for _, sub := range []string{"regress", "plpgsql"} {
		fs, err := filepath.Glob(filepath.Join(corpusDir, sub, "*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, fs...)
	}
	sort.Strings(files)
	if len(files) < 200 {
		t.Fatalf("corpus looks incomplete: only %d files in %s", len(files), corpusDir)
	}

	actual := map[string]map[string]int{} // file -> category -> count
	samples := map[string][]string{}      // category -> example labels
	record := func(file, category, label string) {
		if actual[file] == nil {
			actual[file] = map[string]int{}
		}
		actual[file][category]++
		if len(samples[category]) < 10 {
			samples[category] = append(samples[category], label)
		}
	}

	var total, skipped int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Key baseline entries by subdir/basename so regress and plpgsql
		// files with the same name can't collide.
		name := filepath.Base(filepath.Dir(f)) + "/" + filepath.Base(f)
		stmts, err := pg_query.SplitWithScanner(string(data), true)
		if err != nil {
			// A few files can't be split at all (e.g. copy2.sql contains
			// inline COPY data that breaks the scanner). Skip them.
			t.Logf("%s: skipped, split failed: %v", name, err)
			continue
		}
		for i, stmt := range stmts {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			total++
			label := fmt.Sprintf("%s#%d", name, i)
			if _, err := pg_query.Parse(stmt); err != nil {
				skipped++
				continue
			}
			category := classify(stmt)
			if category != "" {
				record(name, category, label)
			}
		}
	}
	t.Logf("corpus: %d statements, %d skipped as unparseable standalone", total, skipped)

	got := renderBaseline(actual)
	baselinePath := filepath.Join("testdata", "corpus_baseline.txt")
	if os.Getenv("PGFMT_UPDATE_BASELINE") != "" {
		if err := os.WriteFile(baselinePath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", baselinePath)
		return
	}

	wantBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("missing baseline (run with PGFMT_UPDATE_BASELINE=1 to create): %v", err)
	}
	if diff := diffBaseline(string(wantBytes), got); diff != "" {
		for category, ex := range samples {
			t.Logf("sample %s: %s", category, strings.Join(ex, ", "))
		}
		t.Errorf("corpus results differ from testdata/corpus_baseline.txt "+
			"(regressions AND improvements both require a baseline update; "+
			"rerun with PGFMT_UPDATE_BASELINE=1):\n%s", diff)
	}
}

// classify formats one statement and returns the failure category, or "" if
// the statement formats cleanly.
func classify(stmt string) (category string) {
	out, err := safeFormat(stmt)
	if err != nil {
		if strings.HasPrefix(err.Error(), "panic:") {
			return "panic"
		}
		return "format-error"
	}
	if _, err := pg_query.Parse(out); err != nil {
		return "output-invalid"
	}
	if din, ok := deparsed(stmt); ok {
		if dout, ok := deparsed(out); ok {
			if normalizeForCompare(din) != normalizeForCompare(dout) {
				return "roundtrip-diff"
			}
		}
	}
	out2, err := safeFormat(out)
	if err != nil || out2 != out {
		return "not-idempotent"
	}
	return ""
}

func safeFormat(sql string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return printer.Format(sql)
}

func deparsed(sql string) (string, bool) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return "", false
	}
	out, err := pg_query.Deparse(tree)
	if err != nil {
		return "", false
	}
	return out, true
}

func renderBaseline(actual map[string]map[string]int) string {
	var lines []string
	for file, counts := range actual {
		for category, n := range counts {
			lines = append(lines, fmt.Sprintf("%s\t%s\t%d", file, category, n))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// diffBaseline returns a human-readable diff of baseline lines, empty if equal.
func diffBaseline(want, got string) string {
	parse := func(s string) map[string]string {
		m := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			if line == "" {
				continue
			}
			idx := strings.LastIndexByte(line, '\t')
			m[line[:idx]] = line[idx+1:]
		}
		return m
	}
	w, g := parse(want), parse(got)
	var keys []string
	for k := range w {
		keys = append(keys, k)
	}
	for k := range g {
		if _, ok := w[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		wv, wok := w[k]
		gv, gok := g[k]
		switch {
		case !wok:
			fmt.Fprintf(&b, "new     %s: %s\n", k, gv)
		case !gok:
			fmt.Fprintf(&b, "gone    %s: was %s\n", k, wv)
		case wv != gv:
			fmt.Fprintf(&b, "changed %s: %s -> %s\n", k, wv, gv)
		}
	}
	return b.String()
}

const (
	corpusVersion = "17.5" // must track LIB_PG_QUERY_TAG of pg_query_go in go.mod
	corpusURL     = "https://ftp.postgresql.org/pub/source/v" + corpusVersion + "/postgresql-" + corpusVersion + ".tar.gz"
)

// ensureCorpus downloads and extracts the two SQL test suites from the pinned
// PostgreSQL source tarball into testdata/corpus/<version>/{regress,plpgsql},
// reusing them if already present.
func ensureCorpus(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "corpus", corpusVersion)
	if entries, err := filepath.Glob(filepath.Join(dir, "regress", "*.sql")); err == nil && len(entries) > 0 {
		return dir
	}

	t.Logf("downloading %s", corpusURL)
	resp, err := http.Get(corpusURL)
	if err != nil {
		t.Skipf("cannot download corpus: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("cannot download corpus: HTTP %s", resp.Status)
	}

	gz, err := gzip.NewReader(bufio.NewReaderSize(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "postgresql-" + corpusVersion + "/"
	targets := map[string]string{
		prefix + "src/test/regress/sql/":   "regress",
		prefix + "src/pl/plpgsql/src/sql/": "plpgsql",
	}
	tr := tar.NewReader(gz)
	extracted := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".sql") {
			continue
		}
		for tarDir, sub := range targets {
			rest, ok := strings.CutPrefix(hdr.Name, tarDir)
			if !ok || strings.Contains(rest, "/") {
				continue
			}
			outDir := filepath.Join(dir, sub)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outDir, rest), data, 0o644); err != nil {
				t.Fatal(err)
			}
			extracted++
		}
	}
	if extracted == 0 {
		t.Fatal("no corpus files extracted; tarball layout changed?")
	}
	t.Logf("extracted %d corpus files to %s", extracted, dir)
	return dir
}
