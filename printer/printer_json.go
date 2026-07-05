package printer

// SQL/JSON constructor expressions (PostgreSQL 16+): JSON_OBJECT, JSON_ARRAY,
// JSON_OBJECTAGG, JSON_ARRAYAGG, JSON(), JSON_SCALAR, JSON_SERIALIZE and the
// IS JSON predicate. Clause ordering mirrors deparseJsonObjectConstructor and
// friends in libpg_query's postgres_deparse.c.

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// writeJsonFormat writes " FORMAT JSON [ENCODING utfN]" (with leading space)
// when a format is explicitly specified.
func (output *Printer) writeJsonFormat(f *pg_query.JsonFormat) {
	if f == nil || f.FormatType == pg_query.JsonFormatType_JS_FORMAT_DEFAULT {
		return
	}
	output.Builder.WriteString(" FORMAT JSON")
	switch f.Encoding {
	case pg_query.JsonEncoding_JS_ENC_UTF8:
		output.Builder.WriteString(" ENCODING utf8")
	case pg_query.JsonEncoding_JS_ENC_UTF16:
		output.Builder.WriteString(" ENCODING utf16")
	case pg_query.JsonEncoding_JS_ENC_UTF32:
		output.Builder.WriteString(" ENCODING utf32")
	case pg_query.JsonEncoding_JS_ENC_DEFAULT:
	}
}

// writeJsonOutput writes " RETURNING type [FORMAT JSON ...]" (with leading
// space) when an output clause is present.
func (output *Printer) writeJsonOutput(o *pg_query.JsonOutput) {
	if o == nil {
		return
	}
	output.Builder.WriteString(" RETURNING ")
	output.writeTypeName(o.TypeName)
	if o.Returning != nil {
		output.writeJsonFormat(o.Returning.Format)
	}
}

func (output *Printer) writeJsonValueExpr(v *pg_query.JsonValueExpr) {
	if v == nil {
		return
	}
	output.writeNode(v.RawExpr)
	output.writeJsonFormat(v.Format)
}

func (output *Printer) writeJsonKeyValue(kv *pg_query.JsonKeyValue) {
	if kv == nil {
		return
	}
	output.writeNode(kv.Key)
	output.Builder.WriteString(": ")
	output.writeJsonValueExpr(kv.Value)
}

// writeJsonObjectTrailing writes the ABSENT ON NULL / WITH UNIQUE / RETURNING
// clauses shared by JSON_OBJECT and JSON_OBJECTAGG.
func (output *Printer) writeJsonObjectTrailing(absentOnNull, unique bool, out *pg_query.JsonOutput) {
	if absentOnNull {
		output.Builder.WriteString(" ABSENT ON NULL")
	}
	if unique {
		output.Builder.WriteString(" WITH UNIQUE")
	}
	output.writeJsonOutput(out)
}

func (output *Printer) writeJsonObjectConstructor(n *pg_query.JsonObjectConstructor) {
	output.Builder.WriteString("JSON_OBJECT(")
	// Render the trailing clauses separately: they need a leading space after
	// a pair but none at the start of a line (or of an empty argument list).
	var trailing string
	if n.AbsentOnNull || n.Unique || n.Output != nil {
		sub := &Printer{Builder: &strings.Builder{}, indent: output.indent}
		sub.writeJsonObjectTrailing(n.AbsentOnNull, n.Unique, n.Output)
		trailing = sub.Builder.String()
	}
	if len(n.Exprs) >= 2 {
		// One key/value pair per line, like json_build_object.
		output.indent++
		for i, kv := range n.Exprs {
			output.writeNewlineIndent()
			output.writeNode(kv)
			if i != len(n.Exprs)-1 {
				output.Builder.WriteString(",")
			}
		}
		if trailing != "" {
			output.writeNewlineIndent()
			output.Builder.WriteString(strings.TrimPrefix(trailing, " "))
		}
		output.indent--
		output.writeNewlineIndent()
	} else {
		for i, kv := range n.Exprs {
			output.writeNode(kv)
			if i != len(n.Exprs)-1 {
				output.Builder.WriteString(", ")
			}
		}
		if len(n.Exprs) == 0 {
			output.Builder.WriteString(strings.TrimPrefix(trailing, " "))
		} else {
			output.Builder.WriteString(trailing)
		}
	}
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonArrayConstructor(n *pg_query.JsonArrayConstructor) {
	output.Builder.WriteString("JSON_ARRAY(")
	for i, v := range n.Exprs {
		output.writeNode(v)
		if i != len(n.Exprs)-1 {
			output.Builder.WriteString(", ")
		}
	}
	// The grammar default for JSON_ARRAY is ABSENT ON NULL; only an explicit
	// NULL ON NULL needs to be preserved.
	if !n.AbsentOnNull {
		if len(n.Exprs) > 0 {
			output.Builder.WriteString(" ")
		}
		output.Builder.WriteString("NULL ON NULL")
	}
	output.writeJsonOutput(n.Output)
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonArrayQueryConstructor(n *pg_query.JsonArrayQueryConstructor) {
	output.Builder.WriteString("JSON_ARRAY(")
	output.indent++
	output.writeNewlineIndent()
	output.writeNode(n.Query)
	output.writeJsonFormat(n.Format)
	output.writeJsonOutput(n.Output)
	output.indent--
	output.writeNewlineIndent()
	output.Builder.WriteString(")")
}

// writeJsonAggClauses writes the FILTER and OVER clauses of a JSON aggregate.
func (output *Printer) writeJsonAggClauses(c *pg_query.JsonAggConstructor) {
	if c == nil {
		return
	}
	if c.AggFilter != nil {
		output.Builder.WriteString(" FILTER (WHERE ")
		output.writeNode(c.AggFilter)
		output.Builder.WriteString(")")
	}
	if c.Over != nil {
		output.Builder.WriteString(" OVER ")
		over := c.Over
		if over.Name != "" && len(over.PartitionClause) == 0 && len(over.OrderClause) == 0 && over.Refname == "" {
			output.Builder.WriteString(over.Name)
		} else {
			output.writeWindowDef(over)
		}
	}
}

func (output *Printer) writeJsonObjectAgg(n *pg_query.JsonObjectAgg) {
	output.Builder.WriteString("JSON_OBJECTAGG(")
	output.writeJsonKeyValue(n.Arg)
	var out *pg_query.JsonOutput
	if n.Constructor != nil {
		out = n.Constructor.Output
	}
	output.writeJsonObjectTrailing(n.AbsentOnNull, n.Unique, out)
	output.Builder.WriteString(")")
	output.writeJsonAggClauses(n.Constructor)
}

func (output *Printer) writeJsonArrayAgg(n *pg_query.JsonArrayAgg) {
	output.Builder.WriteString("JSON_ARRAYAGG(")
	output.writeJsonValueExpr(n.Arg)
	if n.Constructor != nil && len(n.Constructor.AggOrder) > 0 {
		output.Builder.WriteString(" ORDER BY ")
		output.writeCommaSeparatedList(n.Constructor.AggOrder)
	}
	// The grammar default for JSON_ARRAYAGG is ABSENT ON NULL.
	if !n.AbsentOnNull {
		output.Builder.WriteString(" NULL ON NULL")
	}
	if n.Constructor != nil {
		output.writeJsonOutput(n.Constructor.Output)
	}
	output.Builder.WriteString(")")
	output.writeJsonAggClauses(n.Constructor)
}

func (output *Printer) writeJsonParseExpr(n *pg_query.JsonParseExpr) {
	output.Builder.WriteString("JSON(")
	output.writeJsonValueExpr(n.Expr)
	if n.UniqueKeys {
		output.Builder.WriteString(" WITH UNIQUE KEYS")
	}
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonScalarExpr(n *pg_query.JsonScalarExpr) {
	output.Builder.WriteString("JSON_SCALAR(")
	output.writeNode(n.Expr)
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonSerializeExpr(n *pg_query.JsonSerializeExpr) {
	output.Builder.WriteString("JSON_SERIALIZE(")
	output.writeJsonValueExpr(n.Expr)
	output.writeJsonOutput(n.Output)
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonBehavior(b *pg_query.JsonBehavior) {
	switch b.Btype {
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_NULL:
		output.Builder.WriteString("NULL")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_ERROR:
		output.Builder.WriteString("ERROR")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_EMPTY:
		output.Builder.WriteString("EMPTY")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_TRUE:
		output.Builder.WriteString("TRUE")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_FALSE:
		output.Builder.WriteString("FALSE")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_UNKNOWN:
		output.Builder.WriteString("UNKNOWN")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_ARRAY:
		output.Builder.WriteString("EMPTY ARRAY")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_OBJECT:
		output.Builder.WriteString("EMPTY OBJECT")
	case pg_query.JsonBehaviorType_JSON_BEHAVIOR_DEFAULT:
		output.Builder.WriteString("DEFAULT ")
		output.writeNode(b.Expr)
	default:
		warn("unsupported json behavior: %s", b.Btype.String())
	}
}

// writeJsonFuncExpr writes JSON_EXISTS / JSON_QUERY / JSON_VALUE.
func (output *Printer) writeJsonFuncExpr(n *pg_query.JsonFuncExpr) {
	switch n.Op {
	case pg_query.JsonExprOp_JSON_EXISTS_OP:
		output.Builder.WriteString("JSON_EXISTS(")
	case pg_query.JsonExprOp_JSON_QUERY_OP:
		output.Builder.WriteString("JSON_QUERY(")
	case pg_query.JsonExprOp_JSON_VALUE_OP:
		output.Builder.WriteString("JSON_VALUE(")
	case pg_query.JsonExprOp_JSON_TABLE_OP:
		output.Builder.WriteString("JSON_TABLE(")
	default:
		warn("unsupported json func op: %s", n.Op.String())
		output.Builder.WriteString("(")
	}
	output.writeJsonValueExpr(n.ContextItem)
	output.Builder.WriteString(", ")
	output.writeNode(n.Pathspec)
	for i, p := range n.Passing {
		if i == 0 {
			output.Builder.WriteString(" PASSING ")
		} else {
			output.Builder.WriteString(", ")
		}
		arg := p.GetJsonArgument()
		if arg == nil {
			warn("invalid json passing argument")
			continue
		}
		output.writeJsonValueExpr(arg.Val)
		output.Builder.WriteString(" AS ")
		output.Builder.WriteString(quoteIdentifier(arg.Name))
	}
	output.writeJsonOutput(n.Output)
	switch n.Wrapper {
	case pg_query.JsonWrapper_JSW_NONE:
		output.Builder.WriteString(" WITHOUT WRAPPER")
	case pg_query.JsonWrapper_JSW_CONDITIONAL:
		output.Builder.WriteString(" WITH CONDITIONAL WRAPPER")
	case pg_query.JsonWrapper_JSW_UNCONDITIONAL:
		output.Builder.WriteString(" WITH UNCONDITIONAL WRAPPER")
	case pg_query.JsonWrapper_JSW_UNSPEC:
	}
	switch n.Quotes {
	case pg_query.JsonQuotes_JS_QUOTES_KEEP:
		output.Builder.WriteString(" KEEP QUOTES")
	case pg_query.JsonQuotes_JS_QUOTES_OMIT:
		output.Builder.WriteString(" OMIT QUOTES")
	case pg_query.JsonQuotes_JS_QUOTES_UNSPEC:
	}
	if n.OnEmpty != nil {
		output.Builder.WriteString(" ")
		output.writeJsonBehavior(n.OnEmpty)
		output.Builder.WriteString(" ON EMPTY")
	}
	if n.OnError != nil {
		output.Builder.WriteString(" ")
		output.writeJsonBehavior(n.OnError)
		output.Builder.WriteString(" ON ERROR")
	}
	output.Builder.WriteString(")")
}

func (output *Printer) writeJsonIsPredicate(n *pg_query.JsonIsPredicate) {
	output.writeNode(n.Expr)
	output.writeJsonFormat(n.Format)
	output.Builder.WriteString(" IS JSON")
	switch n.ItemType {
	case pg_query.JsonValueType_JS_TYPE_ARRAY:
		output.Builder.WriteString(" ARRAY")
	case pg_query.JsonValueType_JS_TYPE_OBJECT:
		output.Builder.WriteString(" OBJECT")
	case pg_query.JsonValueType_JS_TYPE_SCALAR:
		output.Builder.WriteString(" SCALAR")
	case pg_query.JsonValueType_JS_TYPE_ANY:
	}
	if n.UniqueKeys {
		output.Builder.WriteString(" WITH UNIQUE")
	}
}
