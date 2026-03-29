package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFormatSQL(t *testing.T) {
	input := "select id,name from users where id=1"
	got, err := formatSQL(input)
	if err != nil {
		t.Fatalf("formatSQL: %v", err)
	}
	if !strings.Contains(got, "SELECT") {
		t.Errorf("expected formatted SQL with SELECT, got: %s", got)
	}
	if !strings.HasSuffix(got, ";\n") {
		t.Errorf("expected trailing semicolon+newline, got: %q", got)
	}
}

func TestFormatSQLInvalid(t *testing.T) {
	_, err := formatSQL("SELECT FROM WHERE")
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}
}

func TestEndPosition(t *testing.T) {
	tests := []struct {
		text string
		want Position
	}{
		{"", Position{0, 0}},
		{"hello", Position{0, 5}},
		{"hello\nworld", Position{1, 5}},
		{"a\nb\nc", Position{2, 1}},
	}
	for _, tt := range tests {
		got := endPosition(tt.text)
		if got != tt.want {
			t.Errorf("endPosition(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestOffsetToPosition(t *testing.T) {
	content := "SELECT\nFROM\nWHERE"
	tests := []struct {
		offset int
		want   Position
	}{
		{0, Position{0, 0}},
		{1, Position{0, 0}},
		{7, Position{0, 6}},
		{8, Position{1, 0}},
	}
	for _, tt := range tests {
		got := offsetToPosition(content, tt.offset)
		if got != tt.want {
			t.Errorf("offsetToPosition(_, %d) = %v, want %v", tt.offset, got, tt.want)
		}
	}
}

func TestServerInitializeAndFormat(t *testing.T) {
	// Build a sequence of LSP messages
	var input bytes.Buffer

	writeMsg := func(msg any) {
		data, _ := json.Marshal(msg)
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}

	idNum := func(n int) *json.RawMessage {
		raw := json.RawMessage(fmt.Sprintf("%d", n))
		return &raw
	}

	// 1. initialize
	writeMsg(Request{
		JSONRPC: "2.0",
		ID:      idNum(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"processId":1}`),
	})

	// 2. initialized
	writeMsg(Request{
		JSONRPC: "2.0",
		Method:  "initialized",
	})

	// 3. didOpen
	docParams, _ := json.Marshal(DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        "file:///test.sql",
			LanguageID: "sql",
			Version:    1,
			Text:       "select id from users",
		},
	})
	writeMsg(Request{
		JSONRPC: "2.0",
		Method:  "textDocument/didOpen",
		Params:  json.RawMessage(docParams),
	})

	// 4. formatting
	fmtParams, _ := json.Marshal(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///test.sql"},
	})
	writeMsg(Request{
		JSONRPC: "2.0",
		ID:      idNum(2),
		Method:  "textDocument/formatting",
		Params:  json.RawMessage(fmtParams),
	})

	// 5. shutdown
	writeMsg(Request{
		JSONRPC: "2.0",
		ID:      idNum(3),
		Method:  "shutdown",
	})

	// We can't send "exit" because it calls os.Exit, so we'll just let the reader EOF.

	var output bytes.Buffer
	s := NewServer(&input, &output)
	err := s.Run()
	// Expected: reading message error when input runs out
	if err == nil {
		t.Fatal("expected error from EOF")
	}

	// Parse responses from output
	responses := output.String()

	// Should contain initialize result with formatting capability
	if !strings.Contains(responses, `"documentFormattingProvider":true`) {
		t.Error("expected documentFormattingProvider in initialize response")
	}

	// Should contain formatting result with SELECT
	if !strings.Contains(responses, "SELECT") {
		t.Errorf("expected formatted SQL in response, got: %s", responses)
	}
}
