//go:build !js && !wasip1

package printer

import (
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
)

var protoMarshalOpts = protojson.MarshalOptions{EmitUnpopulated: false}

func Augment(sql string) ([]byte, error) {
	parseResult, err := pgParse(sql)
	if err != nil {
		return nil, err
	}

	scanResult, err := pgScan(sql)
	if err != nil {
		return nil, err
	}

	comments := ExtractComments(sql, scanResult)
	ast := AugmentedAST{Version: int(parseResult.Version)}

	ci := 0
	for _, rawStmt := range parseResult.Stmts {
		stmtEnd := stmtEndPos(rawStmt, int32(len(sql)))
		realStart := FirstRealTokenStart(scanResult, rawStmt.StmtLocation, stmtEnd)

		// Emit leading comments before this statement.
		for ci < len(comments) && comments[ci].start < realStart {
			ast.Stmts = append(ast.Stmts, AugmentedStmt{
				Comment: &AugmentedComment{
					Text: comments[ci].text,
					Type: commentType(comments[ci].text),
				},
			})
			ci++
		}

		// Skip inline comments for now (Task 5 will handle them).
		for ci < len(comments) && comments[ci].start < stmtEnd {
			ci++
		}

		// Marshal the statement node to JSON.
		stmtJSON, err := protoMarshalOpts.Marshal(rawStmt.Stmt)
		if err != nil {
			return nil, err
		}

		ast.Stmts = append(ast.Stmts, AugmentedStmt{
			Stmt:         json.RawMessage(stmtJSON),
			StmtLocation: rawStmt.StmtLocation,
			StmtLen:      rawStmt.StmtLen,
		})
	}

	// Trailing comments.
	for ci < len(comments) {
		ast.Stmts = append(ast.Stmts, AugmentedStmt{
			Comment: &AugmentedComment{
				Text: comments[ci].text,
				Type: commentType(comments[ci].text),
			},
		})
		ci++
	}

	return json.Marshal(ast)
}

func commentType(text string) string {
	if strings.HasPrefix(text, "/*") {
		return "block"
	}
	return "line"
}
