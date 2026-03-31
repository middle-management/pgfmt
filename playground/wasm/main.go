//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/middle-management/pgfmt/printer"
)

func format(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return js.Global().Get("Promise").Call("resolve",
			map[string]any{"error": "missing SQL argument"})
	}
	sql := args[0].String()

	// Return a Promise so the heavy WASM work runs on a goroutine
	// without blocking the browser's main thread.
	handler := js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		go func() {
			result, err := printer.Format(sql)
			if err != nil {
				resolve.Invoke(map[string]any{"error": err.Error()})
			} else {
				resolve.Invoke(map[string]any{"result": result})
			}
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(handler)
	handler.Release()
	return promise
}

func main() {
	js.Global().Set("pgfmtFormat", js.FuncOf(format))

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
