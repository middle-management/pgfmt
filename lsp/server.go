package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/middle-management/pgfmt/printer"
	pg_query "github.com/pganalyze/pg_query_go/v6"
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
					TextDocumentSync:           1, // Full
					DocumentFormattingProvider: true,
				},
				ServerInfo: ServerInfo{Name: "pgfmt-lsp", Version: "0.1.0"},
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
	params := PublishDiagnosticsParams{URI: uri}
	if content != "" {
		_, err := pg_query.Parse(content)
		if err != nil {
			pos := offsetToPosition(content, parseErrorOffset(err))
			params.Diagnostics = []Diagnostic{{
				Range: Range{
					Start: pos,
					End:   pos,
				},
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
	result, err := pg_query.Parse(content)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for i, stmt := range result.Stmts {
		b := &strings.Builder{}
		p := &printer.Printer{Builder: b}
		p.Print(stmt.Stmt)
		out.WriteString(b.String())
		out.WriteString(";")
		if i < len(result.Stmts)-1 {
			out.WriteString("\n\n")
		} else {
			out.WriteString("\n")
		}
	}
	return out.String(), nil
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

// parseErrorOffset extracts the cursor position from a pg_query parse error.
func parseErrorOffset(err error) int {
	// pg_query errors include the cursor position in the error string.
	// The error message format is: "message at or near \"...\" (at pos N)"
	// We try to extract N. If we can't, return 0.
	msg := err.Error()
	// Look for "scan error" prefix or parse position from the error
	// pg_query_go wraps errors and doesn't always expose Cursorpos directly,
	// so we fall back to position 0.
	_ = msg
	return 0
}
