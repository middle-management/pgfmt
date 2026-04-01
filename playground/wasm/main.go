//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"

	"github.com/middle-management/pgfmt/printer"
)

var version = "dev"

func format(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = map[string]any{"error": fmt.Sprintf("internal error: %v", r)}
		}
	}()
	if len(args) < 1 {
		return map[string]any{"error": "missing SQL argument"}
	}
	sql := args[0].String()
	result, err := printer.Format(sql)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"result": result}
}

func main() {
	js.Global().Set("pgfmtFormat", js.FuncOf(format))
	js.Global().Set("pgfmtVersion", version)

	fmt.Printf("pgfmt %s\n", version)

	// Pre-warm: run a trivial format so the expensive libpg_query WASM
	// compilation happens during load, not on the first user action.
	printer.Format("select 1")

	// Signal that WASM is ready.
	if cb := js.Global().Get("onPgfmtReady"); !cb.IsUndefined() {
		cb.Invoke()
	}

	// Block forever.
	select {}
}
