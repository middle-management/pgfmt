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
		p := &Printer{Builder: b, RawStmt: rawStmt}
		p.Print(node)
		out.WriteString(b.String())
		out.WriteString(";\n\n")
	}

	return out.String(), nil
}

// ParsedBody replaces a raw function body string with its parsed form.
type ParsedBody struct {
	Language string            `json:"language"`
	Stmts    []json.RawMessage `json:"stmts,omitempty"`
	PlPgSQL  json.RawMessage   `json:"plpgsql,omitempty"`
	Raw      string            `json:"raw,omitempty"`
}
