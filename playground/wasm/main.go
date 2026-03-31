//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/middle-management/pgfmt/printer"
)

func format(_ js.Value, args []js.Value) any {
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

	// Signal that WASM is ready.
	if cb := js.Global().Get("onPgfmtReady"); !cb.IsUndefined() {
		cb.Invoke()
	}

	// Block forever.
	select {}
}
