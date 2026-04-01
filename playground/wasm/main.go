//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/middle-management/pgfmt/printer"
	"google.golang.org/protobuf/encoding/protojson"
)

var version = "dev"

var jsonOpts = protojson.UnmarshalOptions{DiscardUnknown: true}

// format takes raw SQL — uses JS callbacks for parsing (slow for many statements).
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

// formatParsed takes pre-parsed JSON (parse tree + scan tokens + original SQL).
// No JS callbacks needed — all parsing was done in JS.
func formatParsed(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = map[string]any{"error": fmt.Sprintf("internal error: %v", r)}
		}
	}()
	if len(args) < 3 {
		return map[string]any{"error": "need 3 args: parseJSON, scanJSON, originalSQL"}
	}
	parseJSON := args[0].String()
	scanJSON := args[1].String()
	originalSQL := args[2].String()

	// Unmarshal parse result.
	parseResult := &pg_query.ParseResult{}
	if err := jsonOpts.Unmarshal([]byte(parseJSON), parseResult); err != nil {
		return map[string]any{"error": fmt.Sprintf("parse unmarshal: %v", err)}
	}

	// Unmarshal scan tokens: [[start, end, tokenKind, keywordKind], ...]
	var rawTokens [][4]any
	if err := json.Unmarshal([]byte(scanJSON), &rawTokens); err != nil {
		return map[string]any{"error": fmt.Sprintf("scan unmarshal: %v", err)}
	}
	scanResult := &pg_query.ScanResult{}
	for _, t := range rawTokens {
		start, _ := t[0].(float64)
		end, _ := t[1].(float64)
		tokenKind, _ := t[2].(string)
		keywordKind, _ := t[3].(string)
		scanResult.Tokens = append(scanResult.Tokens, &pg_query.ScanToken{
			Start:       int32(start),
			End:         int32(end),
			Token:       pg_query.Token(pg_query.Token_value[tokenKind]),
			KeywordKind: pg_query.KeywordKind(pg_query.KeywordKind_value[keywordKind]),
		})
	}

	// Run the printer with pre-parsed data.
	result, err := printer.FormatParsed(parseResult, scanResult, originalSQL)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"result": result}
}

func main() {
	js.Global().Set("pgfmtFormat", js.FuncOf(format))
	js.Global().Set("pgfmtFormatParsed", js.FuncOf(formatParsed))
	js.Global().Set("pgfmtVersion", version)

	fmt.Printf("pgfmt %s\n", version)

	if cb := js.Global().Get("onPgfmtReady"); !cb.IsUndefined() {
		cb.Invoke()
	}

	select {}
}
