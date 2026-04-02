package printer

import "encoding/json"

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

// ParsedBody replaces a raw function body string with its parsed form.
type ParsedBody struct {
	Language string            `json:"language"`
	Stmts    []json.RawMessage `json:"stmts,omitempty"`
	PlPgSQL  json.RawMessage   `json:"plpgsql,omitempty"`
	Raw      string            `json:"raw,omitempty"`
}
