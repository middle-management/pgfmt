//go:build !js

package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/pganalyze/pg_query_go/v6/parser"
)

// Server is a minimal LSP server providing SQL formatting and diagnostics.
type Server struct {
	reader *bufio.Reader
	writer io.Writer
	logger *log.Logger
	docs   map[string]string // URI -> content
}

// NewServer creates a new LSP server reading from r and writing to w.
func NewServer(r io.Reader, w io.Writer) *Server {
	return &Server{
		reader: bufio.NewReader(r),
		writer: w,
		logger: log.New(os.Stderr, "[pgfmt-lsp] ", log.LstdFlags),
		docs:   make(map[string]string),
	}
}

// Run starts the main message loop. It blocks until the input stream closes or exit is received.
func (s *Server) Run() error {
	for {
		body, err := ReadMessage(s.reader)
		if err != nil {
			return fmt.Errorf("reading message: %w", err)
		}

		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			s.logger.Printf("invalid JSON: %v", err)
			continue
		}

		s.logger.Printf("received: %s", req.Method)

		switch req.Method {
		case "initialize":
			s.reply(req.ID, InitializeResult{
				Capabilities: ServerCapabilities{
					TextDocumentSync:                1, // Full
					DocumentFormattingProvider:      true,
					DocumentRangeFormattingProvider: true,
				},
				ServerInfo: ServerInfo{Name: "pgfmt-lsp", Version: "0.4.0"},
			})

		case "initialized":
			// no-op

		case "shutdown":
			s.reply(req.ID, nil)

		case "exit":
			os.Exit(0)

		case "textDocument/didOpen":
			var params DidOpenTextDocumentParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				s.logger.Printf("didOpen unmarshal: %v", err)
				continue
			}
			s.docs[params.TextDocument.URI] = params.TextDocument.Text
			s.publishDiagnostics(params.TextDocument.URI, params.TextDocument.Text)

		case "textDocument/didChange":
			var params DidChangeTextDocumentParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				s.logger.Printf("didChange unmarshal: %v", err)
				continue
			}
			if len(params.ContentChanges) > 0 {
				content := params.ContentChanges[len(params.ContentChanges)-1].Text
				s.docs[params.TextDocument.URI] = content
				s.publishDiagnostics(params.TextDocument.URI, content)
			}

		case "textDocument/didClose":
			var params DidCloseTextDocumentParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				s.logger.Printf("didClose unmarshal: %v", err)
				continue
			}
			delete(s.docs, params.TextDocument.URI)
			s.publishDiagnostics(params.TextDocument.URI, "")

		case "textDocument/formatting":
			var params DocumentFormattingParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				s.logger.Printf("formatting unmarshal: %v", err)
				s.replyError(req.ID, -32602, "invalid params")
				continue
			}
			content, ok := s.docs[params.TextDocument.URI]
			if !ok {
				s.reply(req.ID, []TextEdit{})
				continue
			}
			formatted, err := formatSQL(content)
			if err != nil {
				// Can't format invalid SQL, return empty edits
				s.reply(req.ID, []TextEdit{})
				continue
			}
			if formatted == content {
				s.reply(req.ID, []TextEdit{})
				continue
			}
			endPos := endPosition(content)
			s.reply(req.ID, []TextEdit{{
				Range: Range{
					Start: Position{Line: 0, Character: 0},
					End:   endPos,
				},
				NewText: formatted,
			}})

		case "textDocument/rangeFormatting":
			var params DocumentRangeFormattingParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				s.logger.Printf("rangeFormatting unmarshal: %v", err)
				s.replyError(req.ID, -32602, "invalid params")
				continue
			}
			content, ok := s.docs[params.TextDocument.URI]
			if !ok {
				s.reply(req.ID, []TextEdit{})
				continue
			}
			startOff := positionToOffset(content, params.Range.Start)
			endOff := positionToOffset(content, params.Range.End)
			if startOff < 0 || endOff < 0 || endOff < startOff {
				s.reply(req.ID, []TextEdit{})
				continue
			}
			selection := content[startOff:endOff]
			formatted, err := formatSQL(selection)
			if err != nil {
				// Selection isn't a valid standalone SQL fragment.
				s.reply(req.ID, []TextEdit{})
				continue
			}
			if formatted == selection {
				s.reply(req.ID, []TextEdit{})
				continue
			}
			s.reply(req.ID, []TextEdit{{
				Range:   params.Range,
				NewText: formatted,
			}})

		default:
			if req.ID != nil {
				s.replyError(req.ID, -32601, "method not found: "+req.Method)
			}
		}
	}
}

func (s *Server) reply(id *json.RawMessage, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("marshal response: %v", err)
		return
	}
	if err := WriteMessage(s.writer, data); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}

func (s *Server) replyError(id *json.RawMessage, code int, message string) {
	resp := Response{JSONRPC: "2.0", ID: id, Error: &ResponseError{Code: code, Message: message}}
	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("marshal error response: %v", err)
		return
	}
	if err := WriteMessage(s.writer, data); err != nil {
		s.logger.Printf("write error response: %v", err)
	}
}

func (s *Server) notify(method string, params any) {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Printf("marshal notification: %v", err)
		return
	}
	if err := WriteMessage(s.writer, data); err != nil {
		s.logger.Printf("write notification: %v", err)
	}
}

func (s *Server) publishDiagnostics(uri, content string) {
	params := PublishDiagnosticsParams{URI: uri, Diagnostics: []Diagnostic{}}
	if content != "" {
		_, err := pg_query.Parse(content)
		if err != nil {
			start, end := errorRange(content, err)
			params.Diagnostics = []Diagnostic{{
				Range:    Range{Start: start, End: end},
				Severity: DiagnosticSeverityError,
				Source:   "pgfmt",
				Message:  err.Error(),
			}}
		}
	}
	s.notify("textDocument/publishDiagnostics", params)
}

// formatSQL formats SQL content using the pgfmt printer.
func formatSQL(content string) (string, error) {
	return printer.Format(content)
}

// endPosition returns the Position of the end of the given text.
func endPosition(text string) Position {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return Position{}
	}
	return Position{
		Line:      len(lines) - 1,
		Character: len(lines[len(lines)-1]),
	}
}

// offsetToPosition converts a 1-based character offset to a Position.
func offsetToPosition(content string, offset int) Position {
	if offset <= 0 {
		return Position{}
	}
	line, col := 0, 0
	for i, ch := range content {
		if i >= offset-1 {
			break
		}
		if ch == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return Position{Line: line, Character: col}
}

// positionToOffset converts an LSP Position to a 0-based byte offset.
// Returns -1 if the position is out of range.
func positionToOffset(content string, p Position) int {
	line, col := 0, 0
	for i, ch := range content {
		if line == p.Line && col == p.Character {
			return i
		}
		if ch == '\n' {
			if line == p.Line {
				return i
			}
			line++
			col = 0
		} else {
			col++
		}
	}
	if line == p.Line && col == p.Character {
		return len(content)
	}
	return -1
}

// errorRange returns Start/End positions for a parse error. End is advanced
// past the offending word when possible to give the editor a non-empty range.
func errorRange(content string, err error) (Position, Position) {
	off := parseErrorOffset(err)
	start := offsetToPosition(content, off)
	endOff := wordEndOffset(content, off)
	end := offsetToPosition(content, endOff)
	if endOff <= off {
		end = start
	}
	return start, end
}

// parseErrorOffset extracts the 1-based cursor position from a pg_query
// parse error. Returns 0 if the error is not a parser.Error.
func parseErrorOffset(err error) int {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.Cursorpos
	}
	return 0
}

// wordEndOffset returns a 1-based offset just past the identifier or word
// at the given 1-based offset, used to give parse errors a visible range.
func wordEndOffset(content string, offset int) int {
	if offset <= 0 || offset > len(content) {
		return offset
	}
	i := offset - 1
	for i < len(content) {
		ch := content[i]
		isWord := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_'
		if !isWord {
			break
		}
		i++
	}
	if i == offset-1 {
		i = offset
	}
	return i + 1
}
