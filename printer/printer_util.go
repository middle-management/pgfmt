package printer

import (
	"fmt"
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

// writeQuotedIdentifierList writes a comma-separated list of String nodes as
// quoted identifiers (column name lists in constraints, COPY, VACUUM, ...).
func (output *Printer) writeQuotedIdentifierList(l []*pg_query.Node) {
	for i, n := range l {
		if i > 0 {
			output.Builder.WriteString(", ")
		}
		if s := n.GetString_(); s != nil {
			output.Builder.WriteString(quoteIdentifier(s.Sval))
		} else {
			output.writeNode(n)
		}
	}
}

// writeGUCName writes a configuration parameter name, quoting each dotted
// part as needed (SET custom."bad-guc" = ...).
func (output *Printer) writeGUCName(name string) {
	for i, part := range strings.Split(name, ".") {
		if i > 0 {
			output.Builder.WriteString(".")
		}
		output.Builder.WriteString(quoteIdentifier(part))
	}
}

// dollarQuote returns a dollar-quote delimiter that does not collide with the
// body text, preferring plain $$.
func dollarQuote(body string) string {
	for _, tag := range []string{"$$", "$function$", "$body$", "$pgfmt$"} {
		if !strings.Contains(body, tag) {
			return tag
		}
	}
	for i := 1; ; i++ {
		tag := fmt.Sprintf("$pgfmt%d$", i)
		if !strings.Contains(body, tag) {
			return tag
		}
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

// writeDollarQuotedBody renders a function/DO body via render, then wraps it
// in a dollar-quote delimiter that does not collide with the rendered text
// (bodies can themselves contain $$-quoted strings or nested DO blocks).
func (output *Printer) writeDollarQuotedBody(prefix string, render func(p *Printer)) {
	b := &strings.Builder{}
	tmp := &Printer{Builder: b, bodyCache: output.bodyCache, indent: output.indent}
	render(tmp)
	body := b.String()
	tag := dollarQuote(body)
	output.Builder.WriteString(prefix)
	output.Builder.WriteString(tag)
	output.Builder.WriteString(body)
	output.Builder.WriteString("\n")
	output.Builder.WriteString(tag)
}

// writeBExpr writes an expression in a context restricted to the grammar's
// b_expr (column DEFAULT, ...): AND/OR and IN/LIKE/BETWEEN-style operators
// are not allowed there without parentheses.
func (output *Printer) writeBExpr(node *pg_query.Node) {
	needsParens := false
	switch e := node.GetNode().(type) {
	case *pg_query.Node_BoolExpr:
		needsParens = true
	case *pg_query.Node_AExpr:
		switch e.AExpr.Kind {
		case pg_query.A_Expr_Kind_AEXPR_IN,
			pg_query.A_Expr_Kind_AEXPR_LIKE,
			pg_query.A_Expr_Kind_AEXPR_ILIKE,
			pg_query.A_Expr_Kind_AEXPR_SIMILAR,
			pg_query.A_Expr_Kind_AEXPR_BETWEEN,
			pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN,
			pg_query.A_Expr_Kind_AEXPR_BETWEEN_SYM,
			pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM:
			needsParens = true
		}
	}
	if needsParens {
		output.Builder.WriteString("(")
		output.writeNode(node)
		output.Builder.WriteString(")")
	} else {
		output.writeNode(node)
	}
}

// isBareWord reports whether s can appear unquoted as an option value
// (lowercase letters, digits, underscores).
func isBareWord(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// funcNameKeywords are unreserved keywords whose plain function-call syntax
// is special-cased by the grammar, so calling an ordinary function with one
// of these names requires quoting: "normalize"('a', 'b').
var funcNameKeywords = map[string]bool{
	"normalize": true,
	"xmlexists": true,
	"position":  true,
	"extract":   true,
	"treat":     true,
}

// writeFuncName writes a (possibly qualified) function name with quoting.
func (output *Printer) writeFuncName(names []*pg_query.Node) {
	if len(names) == 1 {
		name := names[0].GetString_().GetSval()
		if funcNameKeywords[name] {
			output.Builder.WriteString(`"` + name + `"`)
			return
		}
	}
	output.writeQuotedQualifiedName(names)
}
