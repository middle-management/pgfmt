package printer

import (
	"encoding/json"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

// AugmentedAST is the top-level structure for the augmented parse tree.
type AugmentedAST struct {
	Version int             `json:"version"`
	Stmts   []AugmentedStmt `json:"stmts"`
}

// AugmentedStmt is either a parsed statement or an inter-statement comment.
type AugmentedStmt struct {
	Stmt         json.RawMessage   `json:"stmt,omitempty"`
	StmtLocation int32             `json:"stmt_location,omitempty"`
	StmtLen      int32             `json:"stmt_len,omitempty"`
	Comment      *AugmentedComment `json:"comment,omitempty"`
	Deparsed     string            `json:"deparsed,omitempty"`
}

// AugmentedComment represents a SQL comment injected into the AST.
type AugmentedComment struct {
	Text string `json:"text"`
	Type string `json:"type"` // "line" or "block"
}

// FormatAugmented reads augmented AST JSON and produces formatted SQL
// using the existing printer. The round-trip invariant is:
// FormatAugmented(Augment(sql)) == Format(sql)
func FormatAugmented(data []byte) (string, error) {
	var ast AugmentedAST
	if err := json.Unmarshal(data, &ast); err != nil {
		return "", fmt.Errorf("augmented AST unmarshal: %w", err)
	}

	var out strings.Builder
	for _, entry := range ast.Stmts {
		if entry.Comment != nil {
			out.WriteString(entry.Comment.Text)
			out.WriteString("\n")
			continue
		}
		if entry.Stmt == nil {
			continue
		}

		// Extract pre-parsed bodies.
		bodyCache := extractBodies(entry.Stmt)

		// Extract inline comments from augmented JSON before protojson unmarshal.
		inlineComments := extractInlineComments(entry.Stmt)

		// Unmarshal the protojson node back into a pg_query.Node.
		unmarshalOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
		node := &pg_query.Node{}
		if err := unmarshalOpts.Unmarshal(entry.Stmt, node); err != nil {
			return "", fmt.Errorf("stmt unmarshal: %w", err)
		}

		rawStmt := &pg_query.RawStmt{
			Stmt:         node,
			StmtLocation: entry.StmtLocation,
			StmtLen:      entry.StmtLen,
		}

		b := &strings.Builder{}
		p := &Printer{Builder: b, comments: inlineComments, RawStmt: rawStmt, Deparsed: entry.Deparsed, bodyCache: bodyCache}
		p.Print(node)
		out.WriteString(b.String())
		out.WriteString(";\n\n")
	}

	return out.String(), nil
}

// extractInlineComments pulls the _comments array from an augmented stmt JSON.
func extractInlineComments(data json.RawMessage) []comment {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(data, &node); err != nil {
		return nil
	}
	commentsRaw, ok := node["_comments"]
	if !ok {
		return nil
	}
	var jc []struct {
		Text  string `json:"text"`
		Start int32  `json:"start"`
		End   int32  `json:"end"`
	}
	if err := json.Unmarshal(commentsRaw, &jc); err != nil {
		return nil
	}
	var result []comment
	for _, c := range jc {
		result = append(result, comment{text: c.Text, start: c.Start, end: c.End})
	}
	return result
}

// extractBodies pulls the _bodies object from an augmented stmt JSON and
// flattens it into a single map for the printer's bodyCache.
func extractBodies(data json.RawMessage) map[string]string {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(data, &node); err != nil {
		return nil
	}
	bodiesRaw, ok := node["_bodies"]
	if !ok {
		return nil
	}
	var bodies map[string]map[string]string
	if err := json.Unmarshal(bodiesRaw, &bodies); err != nil {
		return nil
	}
	// Flatten into a single map for the printer's bodyCache.
	result := make(map[string]string)
	for _, m := range bodies {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// ParsedBody replaces a raw function body string with its parsed form.
type ParsedBody struct {
	Language string            `json:"language"`
	Stmts    []json.RawMessage `json:"stmts,omitempty"`
	PlPgSQL  json.RawMessage   `json:"plpgsql,omitempty"`
	Raw      string            `json:"raw,omitempty"`
}
