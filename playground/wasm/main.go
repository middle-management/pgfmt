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
	result, err := printer.Format(args[0].String())
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"result": result}
}

func main() {
	js.Global().Set("pgfmtFormat", js.FuncOf(format))
	js.Global().Set("pgfmtVersion", version)

	fmt.Printf("pgfmt %s\n", version)

	if cb := js.Global().Get("onPgfmtReady"); !cb.IsUndefined() {
		cb.Invoke()
	}

	select {}
}
