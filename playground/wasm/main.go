//go:build js && wasm

package main

import (
	"fmt"
	"strings"
	"syscall/js"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/middle-management/pgfmt/printer"
	"google.golang.org/protobuf/encoding/protojson"
)

var version = "dev"

var unmarshalOpts = protojson.UnmarshalOptions{DiscardUnknown: true}

// printParseResult takes a pg_query JSON parse result and returns formatted SQL.
func printParseResult(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = map[string]any{"error": fmt.Sprintf("internal error: %v", r)}
		}
	}()
	if len(args) < 1 {
		return map[string]any{"error": "missing parse result JSON"}
	}
	jsonStr := args[0].String()

	result := &pg_query.ParseResult{}
	if err := unmarshalOpts.Unmarshal([]byte(jsonStr), result); err != nil {
		return map[string]any{"error": fmt.Sprintf("unmarshal error: %v", err)}
	}

	var out strings.Builder
	for _, stmt := range result.Stmts {
		b := &strings.Builder{}
		p := &printer.Printer{Builder: b}
		p.Print(stmt.Stmt)
		out.WriteString(b.String())
		out.WriteString(";\n\n")
	}

	return map[string]any{"result": out.String()}
}

func main() {
	js.Global().Set("pgfmtPrintParseResult", js.FuncOf(printParseResult))
	js.Global().Set("pgfmtVersion", version)

	fmt.Printf("pgfmt %s\n", version)

	if cb := js.Global().Get("onPgfmtReady"); !cb.IsUndefined() {
		cb.Invoke()
	}

	select {}
}
