//go:build !js

package printer

import (
	"encoding/json"
	"testing"
)

func TestAugmentInterStatementComments(t *testing.T) {
	sql := "-- header\nSELECT 1;\n-- footer"
	data, err := Augment(sql)
	if err != nil {
		t.Fatal(err)
	}
	var ast AugmentedAST
	if err := json.Unmarshal(data, &ast); err != nil {
		t.Fatal(err)
	}
	// Expect: comment, stmt, comment
	if len(ast.Stmts) != 3 {
		t.Fatalf("expected 3 entries, got %d: %s", len(ast.Stmts), string(data))
	}
	if ast.Stmts[0].Comment == nil || ast.Stmts[0].Comment.Text != "-- header" {
		t.Fatalf("expected leading comment, got %+v", ast.Stmts[0])
	}
	if ast.Stmts[1].Stmt == nil {
		t.Fatal("expected stmt in position 1")
	}
	if ast.Stmts[2].Comment == nil || ast.Stmts[2].Comment.Text != "-- footer" {
		t.Fatalf("expected trailing comment, got %+v", ast.Stmts[2])
	}
}

func TestAugmentedASTMarshalUnmarshal(t *testing.T) {
	original := AugmentedAST{
		Version: 1,
		Stmts: []AugmentedStmt{
			{
				Comment: &AugmentedComment{
					Text: "-- this is a comment",
					Type: "line",
				},
			},
			{
				Stmt:         json.RawMessage(`{"SelectStmt":{"targetList":[]}}`),
				StmtLocation: 22,
				StmtLen:      30,
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded AugmentedAST
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", decoded.Version, original.Version)
	}
	if len(decoded.Stmts) != 2 {
		t.Fatalf("Stmts length: got %d, want 2", len(decoded.Stmts))
	}

	// Check comment entry
	if decoded.Stmts[0].Comment == nil {
		t.Fatal("Stmts[0].Comment is nil")
	}
	if decoded.Stmts[0].Comment.Text != "-- this is a comment" {
		t.Errorf("Comment.Text: got %q, want %q", decoded.Stmts[0].Comment.Text, "-- this is a comment")
	}
	if decoded.Stmts[0].Comment.Type != "line" {
		t.Errorf("Comment.Type: got %q, want %q", decoded.Stmts[0].Comment.Type, "line")
	}
	if decoded.Stmts[0].Stmt != nil {
		t.Errorf("Stmts[0].Stmt should be nil for comment entry")
	}

	// Check statement entry
	if decoded.Stmts[1].Stmt == nil {
		t.Fatal("Stmts[1].Stmt is nil")
	}
	if decoded.Stmts[1].StmtLocation != 22 {
		t.Errorf("StmtLocation: got %d, want 22", decoded.Stmts[1].StmtLocation)
	}
	if decoded.Stmts[1].StmtLen != 30 {
		t.Errorf("StmtLen: got %d, want 30", decoded.Stmts[1].StmtLen)
	}
	if decoded.Stmts[1].Comment != nil {
		t.Errorf("Stmts[1].Comment should be nil for statement entry")
	}
}
