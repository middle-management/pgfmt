package printer

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

func isOp(s string) bool {
	return strings.ContainsAny(s, "~!@#^&|`?+-*/%<>=")
}

func isReservedKeyword(s string) bool {
	for _, kw := range Keywords {
		if strings.EqualFold(s, kw) {
			return true
		}
	}
	return false
}

func (output *Printer) writeCommaSeparatedList(l []*pg_query.Node) {
	output.writeListWithSeparator(l, ", ")
}

func (output *Printer) writeListWithSeparator(l []*pg_query.Node, separator string) {
	for i, dn := range l {
		output.writeNode(dn)
		if i != len(l)-1 {
			output.Builder.WriteString(separator)
		}
	}
}

// writeQuotedQualifiedName writes a dot-separated qualified name (a list of
// String nodes), quoting each part as needed.
func (output *Printer) writeQuotedQualifiedName(names []*pg_query.Node) {
	for i, n := range names {
		if i > 0 {
			output.Builder.WriteString(".")
		}
		output.Builder.WriteString(quoteIdentifier(n.GetString_().GetSval()))
	}
}

func (output *Printer) formatSQLBody(body string, indentLevel int) {
	var result *pg_query.ParseResult
	var err error

	if cached, ok := output.bodyCache[body]; ok {
		result = &pg_query.ParseResult{}
		if unmarshalErr := protojson.Unmarshal([]byte(cached), result); unmarshalErr != nil {
			warn("failed to unmarshal cached body: %v", unmarshalErr)
			output.Builder.WriteString(body)
			return
		}
	} else {
		result, err = pgParse(body)
		if err != nil {
			warn("failed to parse SQL function body: %v", err)
			output.Builder.WriteString(body)
			return
		}
	}

	for i, stmt := range result.Stmts {
		b := &strings.Builder{}
		p := &Printer{Builder: b, indent: indentLevel}
		p.Print(stmt.Stmt)
		// Add newline + indent before each statement
		output.Builder.WriteString("\n")
		for j := 0; j < indentLevel; j++ {
			output.Builder.WriteString("\t")
		}
		output.Builder.WriteString(b.String())
		if i != len(result.Stmts)-1 {
			output.Builder.WriteString(";")
		}
	}
}

// QuoteIdentifier quotes an "identifier" (e.g. a table or a column name) to be
// used as part of an SQL statement.  For example:
//
//	tblname := "my_table"
//	data := "my_data"
//	quoted := db.QuoteIdentifier(tblname)
//	err := db.Exec(fmt.Sprintf("INSERT INTO %s VALUES ($1)", quoted), data)
//
// Any double quotes in name will be escaped.  The quoted identifier will be
// case sensitive when used in a query.  If the input string contains a zero
// byte, the result will be truncated immediately before it.
// Copied from https://github.com/lib/pq
func quoteIdentifier(name string) string {
	end := strings.IndexRune(name, 0)
	if end > -1 {
		name = name[:end]
	}
	// Only quote if necessary: reserved keywords, uppercase, or special characters
	needsQuoting := isReservedKeyword(name)
	if !needsQuoting {
		for _, c := range name {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
				needsQuoting = true
				break
			}
		}
	}
	if !needsQuoting && len(name) > 0 {
		// Must start with letter or underscore
		c := name[0]
		if !((c >= 'a' && c <= 'z') || c == '_') {
			needsQuoting = true
		}
	}
	if needsQuoting {
		return `"` + strings.Replace(name, `"`, `""`, -1) + `"`
	}
	return name
}
