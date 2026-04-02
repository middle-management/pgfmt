//go:build !js && !wasip1

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/middle-management/pgfmt/printer"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func runWASI(t *testing.T, input []byte, wasmBinary []byte) string {
	t.Helper()
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	var stdout, stderr bytes.Buffer
	config := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(input)).
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithArgs("pgfmt-print")

	_, err := r.InstantiateWithConfig(ctx, wasmBinary, config)
	if err != nil {
		t.Fatalf("WASI execution failed: %v\nstderr: %s", err, stderr.String())
	}

	return stdout.String()
}

func TestWASIEquivalence(t *testing.T) {
	wasmPath := filepath.Join(t.TempDir(), "pgfmt-print.wasm")
	cmd := exec.Command("go", "build", "-o", wasmPath, "./cmd/pgfmt-print")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build WASI binary: %v", out)
	}

	wasmBinary, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("failed to read WASM binary: %v", err)
	}

	fixtures, err := filepath.Glob("testdata/fixtures/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if strings.HasSuffix(f, ".gz") {
			continue
		}
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sql, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}

			want, err := printer.Format(string(sql))
			if err != nil {
				t.Fatalf("Format: %v", err)
			}

			augmented, err := printer.Augment(string(sql))
			if err != nil {
				t.Fatalf("Augment: %v", err)
			}
			got := runWASI(t, augmented, wasmBinary)

			if got != want {
				t.Errorf("WASI output mismatch\n--- got (len=%d) ---\n%s\n--- want (len=%d) ---\n%s", len(got), got, len(want), want)
			}
		})
	}
}
