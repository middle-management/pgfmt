//go:build js && wasm && !tinygo

package printer

import (
	"fmt"
	"syscall/js"
)

func warn(format string, args ...any) {
	if cb := js.Global().Get("onPgfmtWarn"); !cb.IsUndefined() {
		cb.Invoke(fmt.Sprintf(format, args...))
	}
}
