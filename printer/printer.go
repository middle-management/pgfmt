package printer

// https://github.com/pganalyze/libpg_query/blob/13-latest/src/pg_query_deparse.c

import (
	"fmt"
	"strconv"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type Printer struct {
	Builder        *strings.Builder
	indent         int               // current indentation level
	comments       []comment         // inline comments for the current statement
	commentIdx     int               // next inline comment to process
	lastNodeEndPos int               // output position after last node with a source location
	nodeDepth      int               // writeNode recursion depth, distinguishes statement vs expression fallback
	RawStmt        *pg_query.RawStmt // set by Format to enable deparse fallback
	OriginalSQL    string            // original SQL input for raw text fallback
	Deparsed       string            // pre-computed deparsed text for fallback
	// bodyCache maps body text → parse result JSON for pre-parsed function bodies.
	// Used by FormatAugmented to avoid calling pgParse/pgParsePlPgSqlToJSON.
	bodyCache map[string]string
}

func (output *Printer) Print(node *pg_query.Node) {
	output.writeNode(node)
	output.flushRemainingComments()
}

// writeIndent writes the current indentation (tabs).
func (output *Printer) writeIndent() {
	for i := 0; i < output.indent; i++ {
		output.Builder.WriteString("\t")
	}
}

// writeNewlineIndent writes a newline followed by the current indentation.
func (output *Printer) writeNewlineIndent() {
	output.Builder.WriteString("\n")
	output.writeIndent()
}

// nodeLocation extracts the source byte position from a Node.
// Returns -1 if the node type has no location field.
func nodeLocation(node *pg_query.Node) int32 {
	if node == nil {
		return -1
	}
	switch n := node.GetNode().(type) {
	case *pg_query.Node_AConst:
		return n.AConst.GetLocation()
	case *pg_query.Node_AExpr:
		return n.AExpr.GetLocation()
	case *pg_query.Node_ColumnRef:
		return n.ColumnRef.GetLocation()
	case *pg_query.Node_FuncCall:
		return n.FuncCall.GetLocation()
	case *pg_query.Node_TypeCast:
		return n.TypeCast.GetLocation()
	case *pg_query.Node_ParamRef:
		return n.ParamRef.GetLocation()
	case *pg_query.Node_BoolExpr:
		return n.BoolExpr.GetLocation()
	case *pg_query.Node_SubLink:
		return n.SubLink.GetLocation()
	case *pg_query.Node_NullTest:
		return n.NullTest.GetLocation()
	case *pg_query.Node_ResTarget:
		return n.ResTarget.GetLocation()
	case *pg_query.Node_RangeVar:
		return n.RangeVar.GetLocation()
	case *pg_query.Node_NamedArgExpr:
		return n.NamedArgExpr.GetLocation()
	case *pg_query.Node_CoalesceExpr:
		return n.CoalesceExpr.GetLocation()
	case *pg_query.Node_MinMaxExpr:
		return n.MinMaxExpr.GetLocation()
	case *pg_query.Node_CaseExpr:
		return n.CaseExpr.GetLocation()
	case *pg_query.Node_CaseWhen:
		return n.CaseWhen.GetLocation()
	case *pg_query.Node_SortBy:
		return n.SortBy.GetLocation()
	case *pg_query.Node_WindowDef:
		return n.WindowDef.GetLocation()
	case *pg_query.Node_ColumnDef:
		return n.ColumnDef.GetLocation()
	case *pg_query.Node_Constraint:
		return n.Constraint.GetLocation()
	case *pg_query.Node_WithClause:
		return n.WithClause.GetLocation()
	case *pg_query.Node_XmlExpr:
		return n.XmlExpr.GetLocation()
	case *pg_query.Node_SqlvalueFunction:
		return n.SqlvalueFunction.GetLocation()
	case *pg_query.Node_GroupingFunc:
		return n.GroupingFunc.GetLocation()
	case *pg_query.Node_SetToDefault:
		return n.SetToDefault.GetLocation()
	case *pg_query.Node_AArrayExpr:
		return n.AArrayExpr.GetLocation()
	case *pg_query.Node_RowExpr:
		return n.RowExpr.GetLocation()
	case *pg_query.Node_JsonObjectConstructor:
		return n.JsonObjectConstructor.GetLocation()
	case *pg_query.Node_JsonArrayConstructor:
		return n.JsonArrayConstructor.GetLocation()
	case *pg_query.Node_JsonArrayQueryConstructor:
		return n.JsonArrayQueryConstructor.GetLocation()
	case *pg_query.Node_JsonParseExpr:
		return n.JsonParseExpr.GetLocation()
	case *pg_query.Node_JsonScalarExpr:
		return n.JsonScalarExpr.GetLocation()
	case *pg_query.Node_JsonSerializeExpr:
		return n.JsonSerializeExpr.GetLocation()
	case *pg_query.Node_JsonIsPredicate:
		return n.JsonIsPredicate.GetLocation()
	case *pg_query.Node_JsonFuncExpr:
		return n.JsonFuncExpr.GetLocation()
	default:
		return -1
	}
}

// emitInlineCommentsUpTo emits all pending inline comments whose source
// position is before pos, inserting them into the output buffer at the
// appropriate location (before the first newline after the last node's output).
func (output *Printer) emitInlineCommentsUpTo(pos int32) {
	if len(output.comments) == 0 || output.commentIdx >= len(output.comments) {
		return
	}
	if output.comments[output.commentIdx].start >= pos {
		return
	}

	s := output.Builder.String()
	insertPos := output.lastNodeEndPos

	// Find the first newline at or after insertPos
	nlOffset := strings.Index(s[insertPos:], "\n")

	// Get indentation from that newline (for subsequent comment lines)
	indentStr := ""
	if nlOffset >= 0 {
		nlAbs := insertPos + nlOffset
		for i := nlAbs + 1; i < len(s); i++ {
			if s[i] == '\t' || s[i] == ' ' {
				indentStr += string(s[i])
			} else {
				break
			}
		}
	}

	var toInsert strings.Builder
	first := true
	for output.commentIdx < len(output.comments) && output.comments[output.commentIdx].start < pos {
		c := output.comments[output.commentIdx]
		if first && nlOffset >= 0 {
			// First comment: place on the same line as the previous node
			toInsert.WriteString(" ")
			toInsert.WriteString(c.text)
		} else {
			// Subsequent comments or no newline: place on new indented lines
			toInsert.WriteString("\n")
			toInsert.WriteString(indentStr)
			toInsert.WriteString(c.text)
		}
		first = false
		output.commentIdx++
	}

	if toInsert.Len() > 0 {
		if nlOffset >= 0 {
			// Insert before the newline
			nlAbs := insertPos + nlOffset
			output.Builder.Reset()
			output.Builder.WriteString(s[:nlAbs])
			output.Builder.WriteString(toInsert.String())
			output.Builder.WriteString(s[nlAbs:])
		} else {
			// No newline found; append at the end. A trailing line comment
			// would swallow whatever the printer writes next, so terminate
			// the comment's line.
			output.Builder.WriteString(toInsert.String())
			if strings.Contains(toInsert.String(), "--") {
				output.Builder.WriteString("\n")
				output.Builder.WriteString(indentStr)
			}
		}
	}
}

// flushRemainingComments emits any comments not yet emitted, using
// retroactive insertion to place them at the correct output position.
func (output *Printer) flushRemainingComments() {
	output.emitInlineCommentsUpTo(1<<31 - 1)
}

type nodeContext int

const (
	nodeContextNone           = iota
	nodeContextInsertRelation // Parent node type (and sometimes field)
	nodeContextInsertOnConflict
	nodeContextUpdate
	nodeContextReturning
	nodeContextAExpr
	nodeContextXmlattributes
	nodeContextXmlnamespaces
	nodeContextCreateType
	nodeContextAlterType
	nodeContextIdentifier // Identifier vs constant context
	nodeContextConstant
)

type nodeOption struct {
	context nodeContext
}

type option func(*nodeOption)

func withNodeContext(ctx nodeContext) option {
	return func(o *nodeOption) {
		o.context = ctx
	}
}

func (output *Printer) writeNode(node *pg_query.Node, opts ...option) {
	loc := nodeLocation(node)
	if loc > 0 && len(output.comments) > 0 {
		output.emitInlineCommentsUpTo(loc)
	}
	output.nodeDepth++
	defer func() {
		output.nodeDepth--
		if loc > 0 {
			output.lastNodeEndPos = output.Builder.Len()
		}
	}()

	o := &nodeOption{}
	for _, opt := range opts {
		opt(o)
	}
	switch n := node.GetNode().(type) {
	case *pg_query.Node_NamedArgExpr:
		output.Builder.WriteString(n.NamedArgExpr.Name)
		output.Builder.WriteString(" := ")
		output.writeNode(n.NamedArgExpr.Arg)
	case *pg_query.Node_List:
		output.writeListWithSeparator(n.List.Items, ", ")
	case *pg_query.Node_SubLink:
		switch n.SubLink.SubLinkType {
		case pg_query.SubLinkType_EXISTS_SUBLINK:
			output.Builder.WriteString("EXISTS (")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ALL_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" ALL (")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ANY_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			if len(n.SubLink.OperName) > 0 {
				output.writeSubqueryOp(n.SubLink.OperName)
				output.Builder.WriteString(" ANY (")
			} else {
				output.Builder.WriteString("IN (")
			}
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ROWCOMPARE_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" (")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_MULTIEXPR_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" (")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ARRAY_SUBLINK:
			output.Builder.WriteString("ARRAY(")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_EXPR_SUBLINK:
			output.Builder.WriteString("(")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_CTE_SUBLINK:
			output.Builder.WriteString("/* UNSUPPORTED: CTE sublink */")
		default:
			warn("unsupported sublink type: %s", n.SubLink.SubLinkType.String())
		}
	case *pg_query.Node_NullTest:
		output.writeNode(n.NullTest.Arg)
		switch n.NullTest.Nulltesttype {
		case pg_query.NullTestType_IS_NOT_NULL:
			output.Builder.WriteString(" IS NOT NULL")
		case pg_query.NullTestType_IS_NULL:
			output.Builder.WriteString(" IS NULL")
		}
	case *pg_query.Node_CollateClause:
		output.writeExprWithParensIfNeeded(n.CollateClause.Arg)
		output.Builder.WriteString(" COLLATE ")
		output.writeQuotedQualifiedName(n.CollateClause.Collname)
	case *pg_query.Node_BooleanTest:
		// AND/OR and comparison arguments need parens: IS binds tighter
		// than AND/OR, so "a AND b IS TRUE" would change meaning.
		output.writeExprWithParensIfNeeded(n.BooleanTest.Arg)
		switch n.BooleanTest.Booltesttype {
		case pg_query.BoolTestType_IS_TRUE:
			output.Builder.WriteString(" IS TRUE")
		case pg_query.BoolTestType_IS_NOT_TRUE:
			output.Builder.WriteString(" IS NOT TRUE")
		case pg_query.BoolTestType_IS_FALSE:
			output.Builder.WriteString(" IS FALSE")
		case pg_query.BoolTestType_IS_NOT_FALSE:
			output.Builder.WriteString(" IS NOT FALSE")
		case pg_query.BoolTestType_IS_UNKNOWN:
			output.Builder.WriteString(" IS UNKNOWN")
		case pg_query.BoolTestType_IS_NOT_UNKNOWN:
			output.Builder.WriteString(" IS NOT UNKNOWN")
		default:
			warn("unsupported boolean test type: %s", n.BooleanTest.Booltesttype.String())
		}
	case *pg_query.Node_Integer:
		output.Builder.WriteString(strconv.Itoa(int(n.Integer.Ival)))
	case *pg_query.Node_Float:
		output.Builder.WriteString(n.Float.Fval)
	case *pg_query.Node_Boolean:
		if n.Boolean.Boolval {
			output.Builder.WriteString("true")
		} else {
			output.Builder.WriteString("false")
		}
	case *pg_query.Node_ParamRef:
		output.Builder.WriteString("$")
		output.Builder.WriteString(strconv.Itoa(int(n.ParamRef.Number)))

	case *pg_query.Node_AConst:
		switch v := n.AConst.Val.(type) {
		case *pg_query.A_Const_Ival:
			output.writeNode(&pg_query.Node{Node: &pg_query.Node_Integer{Integer: v.Ival}})
		case *pg_query.A_Const_Fval:
			output.Builder.WriteString(v.Fval.Fval)
		case *pg_query.A_Const_Boolval:
			if v.Boolval.Boolval {
				output.Builder.WriteString("true")
			} else {
				output.Builder.WriteString("false")
			}
		case *pg_query.A_Const_Sval:
			output.Builder.WriteString("'")
			output.Builder.WriteString(strings.ReplaceAll(v.Sval.Sval, "'", "''"))
			output.Builder.WriteString("'")
		case *pg_query.A_Const_Bsval:
			// Stored as a radix prefix character followed by the digits,
			// e.g. "b0101" or "x1F"; the literal syntax is b'0101'.
			bs := v.Bsval.Bsval
			if len(bs) > 0 {
				output.Builder.WriteString(bs[:1])
				output.Builder.WriteString("'")
				output.Builder.WriteString(bs[1:])
				output.Builder.WriteString("'")
			}
		case nil:
			if n.AConst.Isnull {
				output.Builder.WriteString("NULL")
			}
		}
	case *pg_query.Node_AExpr:

		switch n.AExpr.Kind {
		case pg_query.A_Expr_Kind_AEXPR_OP:
			if n.AExpr.Lexpr != nil {
				output.writeAExprOperand(n.AExpr.Lexpr)
				output.Builder.WriteString(" ")
			}
			output.writeQualOp(n.AExpr.Name)
			if n.AExpr.Rexpr != nil {
				output.Builder.WriteString(" ")
				output.writeAExprOperand(n.AExpr.Rexpr)
			}
		case pg_query.A_Expr_Kind_AEXPR_OP_ANY:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" ")
			output.writeQualOp(n.AExpr.Name)
			output.Builder.WriteString(" ANY (")
			output.writeNode(n.AExpr.Rexpr)
			output.Builder.WriteString(")")
		case pg_query.A_Expr_Kind_AEXPR_OP_ALL:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" ")
			output.writeQualOp(n.AExpr.Name)
			output.Builder.WriteString(" ALL (")
			output.writeNode(n.AExpr.Rexpr)
			output.Builder.WriteString(")")
		case pg_query.A_Expr_Kind_AEXPR_DISTINCT:
			output.writeAExprOperand(n.AExpr.Lexpr)
			output.Builder.WriteString(" IS DISTINCT FROM ")
			output.writeAExprOperand(n.AExpr.Rexpr)
		case pg_query.A_Expr_Kind_AEXPR_NOT_DISTINCT:
			output.writeAExprOperand(n.AExpr.Lexpr)
			output.Builder.WriteString(" IS NOT DISTINCT FROM ")
			output.writeAExprOperand(n.AExpr.Rexpr)
		case pg_query.A_Expr_Kind_AEXPR_NULLIF:
			output.Builder.WriteString("NULLIF(")
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(", ")
			output.writeNode(n.AExpr.Rexpr)
			output.Builder.WriteString(")")
		case pg_query.A_Expr_Kind_AEXPR_IN:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" ")
			if len(n.AExpr.Name) > 0 && n.AExpr.Name[0].GetString_().GetSval() == "<>" {
				output.Builder.WriteString("NOT IN (")
			} else {
				output.Builder.WriteString("IN (")
			}
			if l := n.AExpr.Rexpr.GetList(); l != nil {
				output.writeCommaSeparatedList(l.Items)
			} else {
				output.writeNode(n.AExpr.Rexpr)
			}
			output.Builder.WriteString(")")
		case pg_query.A_Expr_Kind_AEXPR_LIKE:
			output.writeNode(n.AExpr.Lexpr)
			if len(n.AExpr.Name) > 0 && n.AExpr.Name[0].GetString_().GetSval() == "!~~" {
				output.Builder.WriteString(" NOT LIKE ")
			} else {
				output.Builder.WriteString(" LIKE ")
			}
			output.writeNode(n.AExpr.Rexpr)
		case pg_query.A_Expr_Kind_AEXPR_ILIKE:
			output.writeNode(n.AExpr.Lexpr)
			if len(n.AExpr.Name) > 0 && n.AExpr.Name[0].GetString_().GetSval() == "!~~*" {
				output.Builder.WriteString(" NOT ILIKE ")
			} else {
				output.Builder.WriteString(" ILIKE ")
			}
			output.writeNode(n.AExpr.Rexpr)
		case pg_query.A_Expr_Kind_AEXPR_SIMILAR:
			output.writeNode(n.AExpr.Lexpr)
			if len(n.AExpr.Name) > 0 && n.AExpr.Name[0].GetString_().GetSval() == "!~" {
				output.Builder.WriteString(" NOT SIMILAR TO ")
			} else {
				output.Builder.WriteString(" SIMILAR TO ")
			}
			// The parser wraps the pattern in pg_catalog.similar_to_escape();
			// printing that call verbatim would wrap again on re-parse.
			if fc := n.AExpr.Rexpr.GetFuncCall(); fc != nil && len(fc.Funcname) == 2 &&
				fc.Funcname[1].GetString_().GetSval() == "similar_to_escape" &&
				len(fc.Args) >= 1 && len(fc.Args) <= 2 {
				output.writeNode(fc.Args[0])
				if len(fc.Args) == 2 {
					output.Builder.WriteString(" ESCAPE ")
					output.writeNode(fc.Args[1])
				}
			} else {
				output.writeNode(n.AExpr.Rexpr)
			}
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" BETWEEN ")
			if l := n.AExpr.Rexpr.GetList(); l != nil && len(l.Items) == 2 {
				output.writeNode(l.Items[0])
				output.Builder.WriteString(" AND ")
				output.writeNode(l.Items[1])
			}
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" NOT BETWEEN ")
			if l := n.AExpr.Rexpr.GetList(); l != nil && len(l.Items) == 2 {
				output.writeNode(l.Items[0])
				output.Builder.WriteString(" AND ")
				output.writeNode(l.Items[1])
			}
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN_SYM:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" BETWEEN SYMMETRIC ")
			if l := n.AExpr.Rexpr.GetList(); l != nil && len(l.Items) == 2 {
				output.writeNode(l.Items[0])
				output.Builder.WriteString(" AND ")
				output.writeNode(l.Items[1])
			}
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" NOT BETWEEN SYMMETRIC ")
			if l := n.AExpr.Rexpr.GetList(); l != nil && len(l.Items) == 2 {
				output.writeNode(l.Items[0])
				output.Builder.WriteString(" AND ")
				output.writeNode(l.Items[1])
			}
		}

	case *pg_query.Node_FuncCall:
		output.writeFuncName(n.FuncCall.Funcname)
		output.Builder.WriteString("(")
		if n.FuncCall.AggDistinct {
			output.Builder.WriteString("DISTINCT ")
		}
		if n.FuncCall.AggStar {
			output.Builder.WriteString("*")
		}
		if isKeyValuePairCall(n.FuncCall) {
			output.writeKeyValuePairArgs(n.FuncCall.Args)
		} else if breaksArgsForKeyValuePair(n.FuncCall) {
			// One argument per line when an argument is a multi-line
			// key/value call, so nesting stays readable.
			output.indent++
			for i, a := range n.FuncCall.Args {
				output.writeNewlineIndent()
				if n.FuncCall.FuncVariadic && i == len(n.FuncCall.Args)-1 {
					output.Builder.WriteString("VARIADIC ")
				}
				output.writeNode(a)
				if i != len(n.FuncCall.Args)-1 {
					output.Builder.WriteString(",")
				}
			}
			output.indent--
			output.writeNewlineIndent()
		} else {
			for i, a := range n.FuncCall.Args {
				if n.FuncCall.FuncVariadic && i == len(n.FuncCall.Args)-1 {
					output.Builder.WriteString("VARIADIC ")
				}
				output.writeNode(a)

				if i != len(n.FuncCall.Args)-1 {
					output.Builder.WriteString(", ")
				}
			}
		}
		if len(n.FuncCall.AggOrder) > 0 {
			if n.FuncCall.AggWithinGroup {
				output.Builder.WriteString(")")
				output.Builder.WriteString(" WITHIN GROUP (ORDER BY ")
				output.writeCommaSeparatedList(n.FuncCall.AggOrder)
			} else {
				output.Builder.WriteString(" ORDER BY ")
				output.writeCommaSeparatedList(n.FuncCall.AggOrder)
			}
		}
		output.Builder.WriteString(")")
		if n.FuncCall.AggFilter != nil {
			output.Builder.WriteString(" FILTER (WHERE ")
			output.writeNode(n.FuncCall.AggFilter)
			output.Builder.WriteString(")")
		}
		if n.FuncCall.Over != nil {
			output.Builder.WriteString(" OVER ")
			over := n.FuncCall.Over
			if over.Name != "" && len(over.PartitionClause) == 0 && len(over.OrderClause) == 0 && over.Refname == "" {
				// Simple window reference: OVER w
				output.Builder.WriteString(over.Name)
			} else {
				output.writeWindowDef(over)
			}
		}

	case *pg_query.Node_SortBy:
		output.writeNode(n.SortBy.Node)
		switch n.SortBy.SortbyDir {
		case pg_query.SortByDir_SORTBY_ASC:
			output.Builder.WriteString(" ASC")
		case pg_query.SortByDir_SORTBY_DESC:
			output.Builder.WriteString(" DESC")
		case pg_query.SortByDir_SORTBY_USING:
			output.Builder.WriteString(" USING ")
			output.writeQualOp(n.SortBy.UseOp)
		case pg_query.SortByDir_SORTBY_DEFAULT:
		}
		switch n.SortBy.SortbyNulls {
		case pg_query.SortByNulls_SORTBY_NULLS_FIRST:
			output.Builder.WriteString(" NULLS FIRST")
		case pg_query.SortByNulls_SORTBY_NULLS_LAST:
			output.Builder.WriteString(" NULLS LAST")
		case pg_query.SortByNulls_SORTBY_NULLS_DEFAULT:
		}

	case *pg_query.Node_String_:
		output.Builder.WriteString(n.String_.Sval)

	case *pg_query.Node_ColumnRef:
		for i, f := range n.ColumnRef.Fields {
			if i > 0 {
				output.Builder.WriteString(".")
			}
			if s := f.GetString_(); s != nil {
				output.Builder.WriteString(quoteIdentifier(s.Sval))
			} else {
				output.writeNode(f)
			}
		}

	case *pg_query.Node_CommonTableExpr:
		output.Builder.WriteString(quoteIdentifier(n.CommonTableExpr.Ctename))

		if len(n.CommonTableExpr.Aliascolnames) > 0 {
			output.Builder.WriteString("(")
			for i, f := range n.CommonTableExpr.Aliascolnames {
				if i > 0 {
					output.Builder.WriteString(", ")
				}
				output.Builder.WriteString(quoteIdentifier(f.GetString_().GetSval()))
			}
			output.Builder.WriteString(")")
		}
		output.Builder.WriteString(" AS ")

		switch n.CommonTableExpr.Ctematerialized {
		case pg_query.CTEMaterialize_CTEMaterializeDefault:
			// no option
		case pg_query.CTEMaterialize_CTEMaterializeAlways:
			output.Builder.WriteString("MATERIALIZED ")
		case pg_query.CTEMaterialize_CTEMaterializeNever:
			output.Builder.WriteString("NOT MATERIALIZED ")
		}
		output.Builder.WriteString("(")
		output.indent++
		output.writeNewlineIndent()
		output.writeNode(n.CommonTableExpr.Ctequery)
		output.indent--
		output.writeNewlineIndent()
		output.Builder.WriteString(")")

	case *pg_query.Node_CoalesceExpr:
		output.Builder.WriteString("COALESCE(")
		output.writeCommaSeparatedList(n.CoalesceExpr.Args)
		output.Builder.WriteString(")")

	// case *pg_query.Node_TypeCast:

	case *pg_query.Node_IndexElem:
		if n.IndexElem.Name != "" {
			output.Builder.WriteString(quoteIdentifier(n.IndexElem.Name))
		} else if n.IndexElem.Expr != nil {
			switch n.IndexElem.Expr.GetNode().(type) {
			case *pg_query.Node_FuncCall,
				*pg_query.Node_SqlvalueFunction,
				*pg_query.Node_CoalesceExpr,
				*pg_query.Node_MinMaxExpr,
				*pg_query.Node_XmlExpr,
				*pg_query.Node_XmlSerialize:
				output.writeNode(n.IndexElem.Expr)
			default:
				output.Builder.WriteString("(")
				output.writeNode(n.IndexElem.Expr)
				output.Builder.WriteString(")")
			}
		} else {
			warn("invalid index elem")
		}

		if len(n.IndexElem.Collation) > 0 {
			output.Builder.WriteString(" COLLATE ")
			output.writeQuotedQualifiedName(n.IndexElem.Collation)
		}

		if len(n.IndexElem.Opclass) > 0 {
			output.Builder.WriteString(" ")
			output.writeListWithSeparator(n.IndexElem.Opclass, ".")
		}

		switch n.IndexElem.Ordering {
		case pg_query.SortByDir_SORTBY_ASC:
			output.Builder.WriteString(" ASC")
		case pg_query.SortByDir_SORTBY_DESC:
			output.Builder.WriteString(" DESC")
		case pg_query.SortByDir_SORTBY_USING:
			warn("SORTBY_USING not allowed in CREATE INDEX")
		case pg_query.SortByDir_SORTBY_DEFAULT:
		}
		switch n.IndexElem.NullsOrdering {
		case pg_query.SortByNulls_SORTBY_NULLS_FIRST:
			output.Builder.WriteString(" NULLS FIRST")
		case pg_query.SortByNulls_SORTBY_NULLS_LAST:
			output.Builder.WriteString(" NULLS LAST")
		case pg_query.SortByNulls_SORTBY_NULLS_DEFAULT:
		}

	case *pg_query.Node_IndexStmt:
		output.Builder.WriteString("CREATE ")
		if n.IndexStmt.Unique {
			output.Builder.WriteString("UNIQUE ")
		}
		output.Builder.WriteString("INDEX ")
		if n.IndexStmt.Concurrent {
			output.Builder.WriteString("CONCURRENTLY ")
		}
		if n.IndexStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		if n.IndexStmt.Idxname != "" {
			output.Builder.WriteString(n.IndexStmt.Idxname)
			output.Builder.WriteString(" ")
		}
		output.Builder.WriteString("ON ")
		output.writeRangeVar(n.IndexStmt.Relation)
		if n.IndexStmt.AccessMethod != "" {
			output.Builder.WriteString(" USING ")
			output.Builder.WriteString(n.IndexStmt.AccessMethod)
			output.Builder.WriteString(" ")
		} else {
			output.Builder.WriteString(" ")
		}

		output.Builder.WriteString("(")
		output.writeCommaSeparatedList(n.IndexStmt.IndexParams)
		output.Builder.WriteString(")")

		if len(n.IndexStmt.IndexIncludingParams) > 0 {
			output.Builder.WriteString(" INCLUDE (")
			output.writeCommaSeparatedList(n.IndexStmt.IndexIncludingParams)
			output.Builder.WriteString(")")
		}

		if len(n.IndexStmt.Options) > 0 {
			output.Builder.WriteString(" WITH (")
			output.writeCommaSeparatedList(n.IndexStmt.Options)
			output.Builder.WriteString(")")
		}

		if n.IndexStmt.TableSpace != "" {
			output.Builder.WriteString(" TABLESPACE ")
			output.Builder.WriteString(n.IndexStmt.TableSpace)
		}

		if n.IndexStmt.WhereClause != nil {
			output.Builder.WriteString(" WHERE ")
			output.writeNode(n.IndexStmt.WhereClause)
		}

	case *pg_query.Node_TableLikeClause:
		output.Builder.WriteString("LIKE ")
		output.writeRangeVar(n.TableLikeClause.Relation)
		const likeAll = 0x7FFFFFFF
		if n.TableLikeClause.Options == likeAll {
			output.Builder.WriteString(" INCLUDING ALL")
		} else {
			// Bit positions follow the CREATE_TABLE_LIKE_* C enum order.
			names := []string{"COMMENTS", "COMPRESSION", "CONSTRAINTS", "DEFAULTS",
				"GENERATED", "IDENTITY", "INDEXES", "STATISTICS", "STORAGE"}
			for bit, name := range names {
				if n.TableLikeClause.Options&(1<<bit) != 0 {
					output.Builder.WriteString(" INCLUDING ")
					output.Builder.WriteString(name)
				}
			}
		}

	case *pg_query.Node_PartitionElem:
		if n.PartitionElem.Name != "" {
			output.Builder.WriteString(quoteIdentifier(n.PartitionElem.Name))
		} else if n.PartitionElem.Expr != nil {
			// The grammar only allows "windowless" function calls without
			// parentheses; anything else must be wrapped.
			bare := false
			switch e := n.PartitionElem.Expr.GetNode().(type) {
			case *pg_query.Node_FuncCall:
				bare = e.FuncCall.Over == nil && e.FuncCall.AggFilter == nil &&
					!e.FuncCall.AggWithinGroup && len(e.FuncCall.AggOrder) == 0
			case *pg_query.Node_SqlvalueFunction,
				*pg_query.Node_CoalesceExpr,
				*pg_query.Node_MinMaxExpr,
				*pg_query.Node_XmlExpr:
				bare = true
			}
			if bare {
				output.writeNode(n.PartitionElem.Expr)
			} else {
				output.Builder.WriteString("(")
				output.writeNode(n.PartitionElem.Expr)
				output.Builder.WriteString(")")
			}
		}
		if len(n.PartitionElem.Collation) > 0 {
			output.Builder.WriteString(" COLLATE ")
			for i, o := range n.PartitionElem.Collation {
				if i > 0 {
					output.Builder.WriteString(".")
				}
				output.Builder.WriteString(quoteIdentifier(o.GetString_().GetSval()))
			}
		}
		if len(n.PartitionElem.Opclass) > 0 {
			output.Builder.WriteString(" ")
			output.writeListWithSeparator(n.PartitionElem.Opclass, ".")
		}

	case *pg_query.Node_PartitionRangeDatum:
		switch n.PartitionRangeDatum.Kind {
		case pg_query.PartitionRangeDatumKind_PARTITION_RANGE_DATUM_MINVALUE:
			output.Builder.WriteString("MINVALUE")
		case pg_query.PartitionRangeDatumKind_PARTITION_RANGE_DATUM_MAXVALUE:
			output.Builder.WriteString("MAXVALUE")
		default:
			output.writeNode(n.PartitionRangeDatum.Value)
		}

	case *pg_query.Node_RangeVar:
		output.writeRangeVar(n.RangeVar)

	case *pg_query.Node_RangeTableSample:
		output.writeNode(n.RangeTableSample.Relation)
		output.Builder.WriteString(" TABLESAMPLE ")
		output.writeListWithSeparator(n.RangeTableSample.Method, ".")
		output.Builder.WriteString(" (")
		output.writeCommaSeparatedList(n.RangeTableSample.Args)
		output.Builder.WriteString(")")
		if n.RangeTableSample.Repeatable != nil {
			output.Builder.WriteString(" REPEATABLE (")
			output.writeNode(n.RangeTableSample.Repeatable)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_CurrentOfExpr:
		output.Builder.WriteString("CURRENT OF ")
		output.Builder.WriteString(quoteIdentifier(n.CurrentOfExpr.CursorName))

	case *pg_query.Node_RangeSubselect:
		if n.RangeSubselect.Lateral {
			output.Builder.WriteString("LATERAL ")
		}
		output.Builder.WriteString("(")
		output.indent += 2
		output.writeNewlineIndent()
		output.writeNode(n.RangeSubselect.Subquery)
		output.indent -= 2
		output.writeNewlineIndent()
		output.Builder.WriteString("\t)")

		if n.RangeSubselect.Alias != nil {
			output.Builder.WriteString(" ")
			output.writeAlias(n.RangeSubselect.Alias)
		}

	case *pg_query.Node_RangeFunction:
		if n.RangeFunction.Lateral {
			output.Builder.WriteString("LATERAL ")
		}
		if n.RangeFunction.IsRowsfrom {
			output.Builder.WriteString("ROWS FROM (")
		}
		// Functions is a list of lists; each inner list is [funcexpr, coldeflist]
		for i, fn := range n.RangeFunction.Functions {
			if i > 0 {
				output.Builder.WriteString(", ")
			}
			if l := fn.GetList(); l != nil && len(l.Items) > 0 {
				output.writeNode(l.Items[0])
				// Per-function column definitions: f() AS (a int, b text)
				if len(l.Items) > 1 {
					if defs := l.Items[1].GetList(); defs != nil && len(defs.Items) > 0 {
						output.Builder.WriteString(" AS (")
						output.writeCommaSeparatedList(defs.Items)
						output.Builder.WriteString(")")
					}
				}
			} else {
				output.writeNode(fn)
			}
		}
		if n.RangeFunction.IsRowsfrom {
			output.Builder.WriteString(")")
		}
		if n.RangeFunction.Ordinality {
			output.Builder.WriteString(" WITH ORDINALITY")
		}
		if n.RangeFunction.Alias != nil {
			output.Builder.WriteString(" ")
			output.writeAlias(n.RangeFunction.Alias)
		}
		// Range-level column definitions: f() AS [alias] (a int, b text)
		if len(n.RangeFunction.Coldeflist) > 0 {
			if n.RangeFunction.Alias == nil {
				output.Builder.WriteString(" AS")
			}
			output.Builder.WriteString(" (")
			output.writeCommaSeparatedList(n.RangeFunction.Coldeflist)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_JoinExpr:
		// A join with its own alias is a parenthesized join: (a JOIN b) AS x.
		// Without the parens the alias would bind to the right-hand table.
		if n.JoinExpr.Alias != nil {
			output.Builder.WriteString("(")
		}
		output.writeNode(n.JoinExpr.Larg)
		natural := ""
		if n.JoinExpr.IsNatural {
			natural = "NATURAL "
		}
		switch n.JoinExpr.Jointype {
		case pg_query.JoinType_JOIN_INNER:
			if !n.JoinExpr.IsNatural && n.JoinExpr.Quals == nil && len(n.JoinExpr.UsingClause) == 0 {
				output.writeNewlineIndent()
				output.Builder.WriteString("\tCROSS JOIN ")
			} else {
				output.writeNewlineIndent()
				output.Builder.WriteString("\t" + natural + "JOIN ")
			}
		case pg_query.JoinType_JOIN_LEFT:
			output.writeNewlineIndent()
			output.Builder.WriteString("\t" + natural + "LEFT JOIN ")
		case pg_query.JoinType_JOIN_FULL:
			output.writeNewlineIndent()
			output.Builder.WriteString("\t" + natural + "FULL JOIN ")
		case pg_query.JoinType_JOIN_RIGHT:
			output.writeNewlineIndent()
			output.Builder.WriteString("\t" + natural + "RIGHT JOIN ")
		default:
			output.writeNewlineIndent()
			output.Builder.WriteString("\t" + natural + "JOIN ")
		}
		output.writeNode(n.JoinExpr.Rarg)
		if n.JoinExpr.Quals != nil {
			output.Builder.WriteString(" ON ")
			output.writeNode(n.JoinExpr.Quals)
		}
		if len(n.JoinExpr.UsingClause) > 0 {
			output.Builder.WriteString(" USING (")
			output.writeCommaSeparatedList(n.JoinExpr.UsingClause)
			output.Builder.WriteString(")")
			if n.JoinExpr.JoinUsingAlias != nil {
				output.Builder.WriteString(" AS ")
				output.writeAlias(n.JoinExpr.JoinUsingAlias)
			}
		}
		if n.JoinExpr.Alias != nil {
			output.Builder.WriteString(") AS ")
			output.writeAlias(n.JoinExpr.Alias)
		}

	case *pg_query.Node_ResTarget:
		output.writeNode(n.ResTarget.Val)
		if n.ResTarget.Name != "" {
			output.Builder.WriteString(" AS ")
			output.Builder.WriteString(quoteIdentifier(n.ResTarget.Name))
		}

	case *pg_query.Node_BoolExpr:
		switch n.BoolExpr.Boolop {
		case pg_query.BoolExprType_AND_EXPR:
			for i, x := range n.BoolExpr.Args {
				output.writeExprWithParensIfNeeded(x)
				if i != len(n.BoolExpr.Args)-1 {
					output.writeNewlineIndent()
					output.Builder.WriteString("AND ")
				}
			}

		case pg_query.BoolExprType_OR_EXPR:
			for i, x := range n.BoolExpr.Args {
				output.writeExprWithParensIfNeeded(x)
				if i != len(n.BoolExpr.Args)-1 {
					output.writeNewlineIndent()
					output.Builder.WriteString("OR ")
				}
			}

		case pg_query.BoolExprType_NOT_EXPR:
			output.Builder.WriteString("NOT ")
			for _, x := range n.BoolExpr.Args {
				output.writeExprWithParensIfNeeded(x)
			}

		}

	case *pg_query.Node_SelectStmt:
		output.writeSelectStmt(n.SelectStmt)

	case *pg_query.Node_InsertStmt:
		if n.InsertStmt.WithClause != nil {
			output.writeWithClause(n.InsertStmt.WithClause)
		}

		output.Builder.WriteString("INSERT INTO")
		output.writeNewlineIndent()
		output.Builder.WriteString("\t")
		output.writeRangeVar(n.InsertStmt.Relation)

		if len(n.InsertStmt.Cols) > 0 {
			output.Builder.WriteString(" (")
			for i, c := range n.InsertStmt.Cols {
				output.Builder.WriteString(quoteIdentifier(c.GetResTarget().Name))
				output.writeOptIndirection(c.GetResTarget().Indirection)
				if i != len(n.InsertStmt.Cols)-1 {
					output.Builder.WriteString(", ")
				}
			}
			output.Builder.WriteString(")")
		}

		switch n.InsertStmt.Override {
		case pg_query.OverridingKind_OVERRIDING_NOT_SET:
			// do nothing
		case pg_query.OverridingKind_OVERRIDING_USER_VALUE:
			output.Builder.WriteString(" OVERRIDING USER VALUE")
		case pg_query.OverridingKind_OVERRIDING_SYSTEM_VALUE:
			output.Builder.WriteString(" OVERRIDING SYSTEM VALUE")
		}

		if n.InsertStmt.SelectStmt != nil {
			output.writeNewlineIndent()
			output.writeNode(n.InsertStmt.SelectStmt)
		} else {
			output.writeNewlineIndent()
			output.Builder.WriteString("DEFAULT VALUES")
		}

		if n.InsertStmt.OnConflictClause != nil {
			output.writeNewlineIndent()
			output.writeOnConflictClause(n.InsertStmt.OnConflictClause)
		}

		if len(n.InsertStmt.ReturningList) > 0 {
			output.writeNewlineIndent()
			output.Builder.WriteString("RETURNING")
			output.indent++
			output.writeNewlineIndent()
			output.writeCommaSeparatedList(n.InsertStmt.ReturningList)
			output.indent--
		}

	case *pg_query.Node_AStar:
		output.Builder.WriteString("*")

	case *pg_query.Node_TypeCast:
		switch n.TypeCast.Arg.Node.(type) {
		case *pg_query.Node_AExpr:
			output.Builder.WriteString("CAST(")
			output.writeNode(n.TypeCast.Arg)
			output.Builder.WriteString(" AS ")
			output.writeTypeName(n.TypeCast.TypeName)
			output.Builder.WriteString(")")
		case *pg_query.Node_AConst:
			// A negative numeric literal must keep its parens: -1::int8
			// re-parses as -(1::int8), a different tree.
			c := n.TypeCast.Arg.GetAConst()
			negative := c.GetIval().GetIval() < 0 || strings.HasPrefix(c.GetFval().GetFval(), "-")
			if negative {
				output.Builder.WriteString("(")
			}
			output.writeNode(n.TypeCast.Arg)
			if negative {
				output.Builder.WriteString(")")
			}
			output.Builder.WriteString("::")
			output.writeTypeName(n.TypeCast.TypeName)
		default:
			output.writeNode(n.TypeCast.Arg)
			output.Builder.WriteString("::")
			output.writeTypeName(n.TypeCast.TypeName)
		}

	case *pg_query.Node_UpdateStmt:
		if n.UpdateStmt.WithClause != nil {
			output.writeWithClause(n.UpdateStmt.WithClause)
		}
		output.Builder.WriteString("UPDATE ")
		output.writeRangeVar(n.UpdateStmt.Relation)
		output.Builder.WriteString("\nSET\n\t")
		output.indent++
		for i := 0; i < len(n.UpdateStmt.TargetList); {
			if i > 0 {
				output.Builder.WriteString(",\n\t")
			}
			rt := n.UpdateStmt.TargetList[i].GetResTarget()
			// A multi-assignment (SET (a, b) = source) arrives as one target
			// per column, each holding a MultiAssignRef to the shared source.
			if mar := rt.Val.GetMultiAssignRef(); mar != nil {
				output.Builder.WriteString("(")
				count := int(mar.Ncolumns)
				for j := 0; j < count && i+j < len(n.UpdateStmt.TargetList); j++ {
					if j > 0 {
						output.Builder.WriteString(", ")
					}
					crt := n.UpdateStmt.TargetList[i+j].GetResTarget()
					output.Builder.WriteString(quoteIdentifier(crt.Name))
					output.writeOptIndirection(crt.Indirection)
				}
				output.Builder.WriteString(") = ")
				output.writeNode(mar.Source)
				i += count
				continue
			}
			output.Builder.WriteString(quoteIdentifier(rt.Name))
			output.writeOptIndirection(rt.Indirection)
			output.Builder.WriteString(" = ")
			output.writeNode(rt.Val)
			i++
		}
		output.indent--
		if len(n.UpdateStmt.FromClause) > 0 {
			output.Builder.WriteString("\nFROM\n\t")
			output.writeCommaSeparatedList(n.UpdateStmt.FromClause)
		}
		if n.UpdateStmt.WhereClause != nil {
			output.Builder.WriteString("\nWHERE\n\t")
			output.writeNode(n.UpdateStmt.WhereClause)
		}
		if len(n.UpdateStmt.ReturningList) > 0 {
			output.Builder.WriteString("\nRETURNING\n\t")
			output.indent++
			output.writeCommaSeparatedList(n.UpdateStmt.ReturningList)
			output.indent--
		}

	case *pg_query.Node_DeleteStmt:
		if n.DeleteStmt.WithClause != nil {
			output.writeWithClause(n.DeleteStmt.WithClause)
		}
		output.Builder.WriteString("DELETE FROM ")
		output.writeRangeVar(n.DeleteStmt.Relation)
		if len(n.DeleteStmt.UsingClause) > 0 {
			output.Builder.WriteString("\nUSING\n\t")
			output.writeCommaSeparatedList(n.DeleteStmt.UsingClause)
		}
		if n.DeleteStmt.WhereClause != nil {
			output.Builder.WriteString("\nWHERE\n\t")
			output.writeNode(n.DeleteStmt.WhereClause)
		}
		if len(n.DeleteStmt.ReturningList) > 0 {
			output.Builder.WriteString("\nRETURNING\n\t")
			output.indent++
			output.writeCommaSeparatedList(n.DeleteStmt.ReturningList)
			output.indent--
		}

	case *pg_query.Node_CreateStmt:
		output.Builder.WriteString("CREATE ")
		if n.CreateStmt.Relation.Relpersistence == "t" {
			output.Builder.WriteString("TEMPORARY ")
		} else if n.CreateStmt.Relation.Relpersistence == "u" {
			output.Builder.WriteString("UNLOGGED ")
		}
		output.Builder.WriteString("TABLE ")
		if n.CreateStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		output.writeRangeVar(n.CreateStmt.Relation)

		// With a partition bound, InhRelations holds the parent table of a
		// PARTITION OF clause rather than INHERITS.
		partitionOf := n.CreateStmt.Partbound != nil && len(n.CreateStmt.InhRelations) > 0
		if partitionOf {
			output.Builder.WriteString(" PARTITION OF ")
			output.writeNode(n.CreateStmt.InhRelations[0])
		} else if n.CreateStmt.OfTypename != nil {
			output.Builder.WriteString(" OF ")
			output.writeTypeName(n.CreateStmt.OfTypename)
		}

		// PARTITION OF and OF type_name tables omit the column list entirely
		// when there are no constraint entries; a plain empty list is valid
		// (and required) otherwise.
		if len(n.CreateStmt.TableElts) > 0 || (!partitionOf && n.CreateStmt.OfTypename == nil) {
			output.Builder.WriteString(" (")
			output.indent++
			for i, elt := range n.CreateStmt.TableElts {
				output.writeNewlineIndent()
				output.writeNode(elt)
				if i != len(n.CreateStmt.TableElts)-1 {
					output.Builder.WriteString(",")
				}
			}
			output.indent--
			output.Builder.WriteString("\n)")
		}

		if !partitionOf && len(n.CreateStmt.InhRelations) > 0 {
			output.Builder.WriteString(" INHERITS (")
			output.writeCommaSeparatedList(n.CreateStmt.InhRelations)
			output.Builder.WriteString(")")
		}
		if n.CreateStmt.Partbound != nil {
			output.writePartitionBound(n.CreateStmt.Partbound)
		}
		if n.CreateStmt.Partspec != nil {
			output.writePartitionSpec(n.CreateStmt.Partspec)
		}
		if n.CreateStmt.AccessMethod != "" {
			output.Builder.WriteString(" USING ")
			output.Builder.WriteString(quoteIdentifier(n.CreateStmt.AccessMethod))
		}
		if len(n.CreateStmt.Options) > 0 {
			output.Builder.WriteString(" WITH (")
			output.writeCommaSeparatedList(n.CreateStmt.Options)
			output.Builder.WriteString(")")
		}
		switch n.CreateStmt.Oncommit {
		case pg_query.OnCommitAction_ONCOMMIT_PRESERVE_ROWS:
			output.Builder.WriteString(" ON COMMIT PRESERVE ROWS")
		case pg_query.OnCommitAction_ONCOMMIT_DELETE_ROWS:
			output.Builder.WriteString(" ON COMMIT DELETE ROWS")
		case pg_query.OnCommitAction_ONCOMMIT_DROP:
			output.Builder.WriteString(" ON COMMIT DROP")
		}
		if n.CreateStmt.Tablespacename != "" {
			output.Builder.WriteString(" TABLESPACE ")
			output.Builder.WriteString(quoteIdentifier(n.CreateStmt.Tablespacename))
		}

	case *pg_query.Node_ColumnDef:
		output.Builder.WriteString(quoteIdentifier(n.ColumnDef.Colname))
		// TypeName is nil for column entries that only attach constraints to
		// an inherited column (CREATE TABLE ... PARTITION OF / OF type_name).
		if n.ColumnDef.TypeName != nil {
			output.Builder.WriteString(" ")
			output.writeTypeName(n.ColumnDef.TypeName)
		}
		if n.ColumnDef.CollClause != nil {
			output.Builder.WriteString(" COLLATE ")
			output.writeQuotedQualifiedName(n.ColumnDef.CollClause.Collname)
		}
		for _, c := range n.ColumnDef.Constraints {
			output.Builder.WriteString(" ")
			output.writeNode(c)
		}

	case *pg_query.Node_Constraint:
		switch n.Constraint.Contype {
		case pg_query.ConstrType_CONSTR_NULL:
			output.Builder.WriteString("NULL")
		case pg_query.ConstrType_CONSTR_NOTNULL:
			output.Builder.WriteString("NOT NULL")
		case pg_query.ConstrType_CONSTR_DEFAULT:
			output.Builder.WriteString("DEFAULT ")
			output.writeBExpr(n.Constraint.RawExpr)
		case pg_query.ConstrType_CONSTR_IDENTITY:
			output.Builder.WriteString("GENERATED ")
			switch n.Constraint.GeneratedWhen {
			case "d":
				output.Builder.WriteString("BY DEFAULT ")
			default:
				output.Builder.WriteString("ALWAYS ")
			}
			output.Builder.WriteString("AS IDENTITY")
		case pg_query.ConstrType_CONSTR_GENERATED:
			output.Builder.WriteString("GENERATED ALWAYS AS (")
			output.writeNode(n.Constraint.RawExpr)
			output.Builder.WriteString(") STORED")
		case pg_query.ConstrType_CONSTR_CHECK:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(quoteIdentifier(n.Constraint.Conname))
				output.Builder.WriteString(" ")
			}
			// Use multiline format for complex boolean expressions
			if be, ok := n.Constraint.RawExpr.GetNode().(*pg_query.Node_BoolExpr); ok && len(be.BoolExpr.Args) > 1 {
				output.Builder.WriteString("CHECK (")
				output.indent++
				output.writeNewlineIndent()
				output.writeNode(n.Constraint.RawExpr)
				output.indent--
				output.writeNewlineIndent()
				output.Builder.WriteString(")")
			} else {
				output.Builder.WriteString("CHECK (")
				output.writeNode(n.Constraint.RawExpr)
				output.Builder.WriteString(")")
			}
			if n.Constraint.IsNoInherit {
				output.Builder.WriteString(" NO INHERIT")
			}
			if n.Constraint.SkipValidation {
				output.Builder.WriteString(" NOT VALID")
			}
		case pg_query.ConstrType_CONSTR_PRIMARY:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(quoteIdentifier(n.Constraint.Conname))
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("PRIMARY KEY")
			if len(n.Constraint.Keys) > 0 {
				output.Builder.WriteString(" (")
				output.writeQuotedIdentifierList(n.Constraint.Keys)
				output.Builder.WriteString(")")
			}
			output.writeIndexConstraintTail(n.Constraint)
		case pg_query.ConstrType_CONSTR_UNIQUE:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(quoteIdentifier(n.Constraint.Conname))
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("UNIQUE")
			if n.Constraint.NullsNotDistinct {
				output.Builder.WriteString(" NULLS NOT DISTINCT")
			}
			if len(n.Constraint.Keys) > 0 {
				output.Builder.WriteString(" (")
				output.writeQuotedIdentifierList(n.Constraint.Keys)
				output.Builder.WriteString(")")
			}
			output.writeIndexConstraintTail(n.Constraint)
		case pg_query.ConstrType_CONSTR_EXCLUSION:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(quoteIdentifier(n.Constraint.Conname))
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("EXCLUDE")
			if n.Constraint.AccessMethod != "" {
				output.Builder.WriteString(" USING ")
				output.Builder.WriteString(n.Constraint.AccessMethod)
			}
			output.Builder.WriteString(" (")
			for i, ex := range n.Constraint.Exclusions {
				// Each exclusion is a two-item list: [IndexElem, operator name list].
				if i > 0 {
					output.Builder.WriteString(", ")
				}
				items := ex.GetList().GetItems()
				if len(items) != 2 {
					warn("unexpected exclusion structure")
					continue
				}
				output.writeNode(items[0])
				output.Builder.WriteString(" WITH ")
				output.writeAnyOperator(items[1].GetList().GetItems())
			}
			output.Builder.WriteString(")")
			if n.Constraint.WhereClause != nil {
				output.Builder.WriteString(" WHERE (")
				output.writeNode(n.Constraint.WhereClause)
				output.Builder.WriteString(")")
			}
			output.writeIndexConstraintTail(n.Constraint)
		case pg_query.ConstrType_CONSTR_ATTR_DEFERRABLE:
			output.Builder.WriteString("DEFERRABLE")
		case pg_query.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE:
			output.Builder.WriteString("NOT DEFERRABLE")
		case pg_query.ConstrType_CONSTR_ATTR_DEFERRED:
			output.Builder.WriteString("INITIALLY DEFERRED")
		case pg_query.ConstrType_CONSTR_ATTR_IMMEDIATE:
			output.Builder.WriteString("INITIALLY IMMEDIATE")
		case pg_query.ConstrType_CONSTR_FOREIGN:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(quoteIdentifier(n.Constraint.Conname))
				output.Builder.WriteString(" ")
			}
			if len(n.Constraint.FkAttrs) > 0 {
				output.Builder.WriteString("FOREIGN KEY (")
				output.writeQuotedIdentifierList(n.Constraint.FkAttrs)
				output.Builder.WriteString(") ")
			}
			output.Builder.WriteString("REFERENCES ")
			output.writeRangeVar(n.Constraint.Pktable)
			if len(n.Constraint.PkAttrs) > 0 {
				output.Builder.WriteString(" (")
				output.writeQuotedIdentifierList(n.Constraint.PkAttrs)
				output.Builder.WriteString(")")
			}
			switch n.Constraint.FkMatchtype {
			case "f":
				output.Builder.WriteString(" MATCH FULL")
			case "p":
				output.Builder.WriteString(" MATCH PARTIAL")
			}
			output.writeFkAction("ON DELETE", n.Constraint.FkDelAction, n.Constraint.FkDelSetCols)
			output.writeFkAction("ON UPDATE", n.Constraint.FkUpdAction, nil)
			if n.Constraint.Deferrable {
				output.Builder.WriteString(" DEFERRABLE")
			}
			if n.Constraint.Initdeferred {
				output.Builder.WriteString(" INITIALLY DEFERRED")
			}
			if n.Constraint.SkipValidation {
				output.Builder.WriteString(" NOT VALID")
			}
		default:
			warn("unsupported constraint type: %s", n.Constraint.Contype.String())
		}

	case *pg_query.Node_AlterTableStmt:
		// If any subcommand is unsupported, emitting a partial statement
		// would produce invalid SQL (e.g. "ALTER TABLE t\n\t;"). Fall back
		// to deparsing the whole statement instead.
		unsupported := false
		for _, cmd := range n.AlterTableStmt.Cmds {
			if c := cmd.GetAlterTableCmd(); c != nil && !alterTableCmdSupported(c) {
				warn("unsupported alter table cmd: %s", c.Subtype.String())
				unsupported = true
			}
		}
		if unsupported && output.tryStatementFallback() {
			return
		}

		output.Builder.WriteString("ALTER ")
		switch n.AlterTableStmt.Objtype {
		case pg_query.ObjectType_OBJECT_INDEX:
			output.Builder.WriteString("INDEX ")
		case pg_query.ObjectType_OBJECT_SEQUENCE:
			output.Builder.WriteString("SEQUENCE ")
		case pg_query.ObjectType_OBJECT_VIEW:
			output.Builder.WriteString("VIEW ")
		case pg_query.ObjectType_OBJECT_MATVIEW:
			output.Builder.WriteString("MATERIALIZED VIEW ")
		case pg_query.ObjectType_OBJECT_FOREIGN_TABLE:
			output.Builder.WriteString("FOREIGN TABLE ")
		default:
			output.Builder.WriteString("TABLE ")
		}
		if n.AlterTableStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
		}
		if !n.AlterTableStmt.Relation.Inh && n.AlterTableStmt.Objtype == pg_query.ObjectType_OBJECT_TABLE {
			output.Builder.WriteString("ONLY ")
		}
		output.writeRangeVar(n.AlterTableStmt.Relation)
		for i, cmd := range n.AlterTableStmt.Cmds {
			if i > 0 {
				output.Builder.WriteString(",")
			}
			output.Builder.WriteString("\n\t")
			output.writeNode(cmd)
		}

	case *pg_query.Node_AlterTableCmd:
		switch n.AlterTableCmd.Subtype {
		case pg_query.AlterTableType_AT_AddColumn:
			output.Builder.WriteString("ADD COLUMN ")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString("IF NOT EXISTS ")
			}
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_DropColumn:
			output.Builder.WriteString("DROP COLUMN ")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString("IF EXISTS ")
			}
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_AlterColumnType:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" TYPE ")
			if def := n.AlterTableCmd.Def; def != nil {
				if cd := def.GetColumnDef(); cd != nil {
					output.writeTypeName(cd.TypeName)
					if cd.CollClause != nil {
						output.Builder.WriteString(" COLLATE ")
						for i, o := range cd.CollClause.Collname {
							if i > 0 {
								output.Builder.WriteString(".")
							}
							output.Builder.WriteString(quoteIdentifier(o.GetString_().GetSval()))
						}
					}
					if cd.RawDefault != nil {
						output.Builder.WriteString(" USING ")
						output.writeNode(cd.RawDefault)
					}
				}
			}
		case pg_query.AlterTableType_AT_ColumnDefault:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			if n.AlterTableCmd.Def != nil {
				output.Builder.WriteString(" SET DEFAULT ")
				output.writeNode(n.AlterTableCmd.Def)
			} else {
				output.Builder.WriteString(" DROP DEFAULT")
			}
		case pg_query.AlterTableType_AT_SetNotNull:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" SET NOT NULL")
		case pg_query.AlterTableType_AT_DropNotNull:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" DROP NOT NULL")
		case pg_query.AlterTableType_AT_AddConstraint:
			output.Builder.WriteString("ADD ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_DropConstraint:
			output.Builder.WriteString("DROP CONSTRAINT ")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString("IF EXISTS ")
			}
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_AddIndex:
			output.Builder.WriteString("ADD INDEX ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_ChangeOwner:
			output.Builder.WriteString("OWNER TO ")
			output.writeRoleSpec(n.AlterTableCmd.Newowner)
		case pg_query.AlterTableType_AT_AddIdentity:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" ADD ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_EnableRowSecurity:
			output.Builder.WriteString("ENABLE ROW LEVEL SECURITY")
		case pg_query.AlterTableType_AT_DisableRowSecurity:
			output.Builder.WriteString("DISABLE ROW LEVEL SECURITY")
		case pg_query.AlterTableType_AT_ForceRowSecurity:
			output.Builder.WriteString("FORCE ROW LEVEL SECURITY")
		case pg_query.AlterTableType_AT_NoForceRowSecurity:
			output.Builder.WriteString("NO FORCE ROW LEVEL SECURITY")
		case pg_query.AlterTableType_AT_ValidateConstraint:
			output.Builder.WriteString("VALIDATE CONSTRAINT ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_AttachPartition:
			pc := n.AlterTableCmd.Def.GetPartitionCmd()
			output.Builder.WriteString("ATTACH PARTITION ")
			output.writeRangeVar(pc.Name)
			if pc.Bound != nil {
				output.writePartitionBound(pc.Bound)
			}
		case pg_query.AlterTableType_AT_DetachPartition:
			pc := n.AlterTableCmd.Def.GetPartitionCmd()
			output.Builder.WriteString("DETACH PARTITION ")
			output.writeRangeVar(pc.Name)
			if pc.Concurrent {
				output.Builder.WriteString(" CONCURRENTLY")
			}
		case pg_query.AlterTableType_AT_DetachPartitionFinalize:
			pc := n.AlterTableCmd.Def.GetPartitionCmd()
			output.Builder.WriteString("DETACH PARTITION ")
			output.writeRangeVar(pc.Name)
			output.Builder.WriteString(" FINALIZE")
		case pg_query.AlterTableType_AT_SetStatistics:
			output.Builder.WriteString("ALTER COLUMN ")
			output.writeAlterColumnRef(n.AlterTableCmd)
			output.Builder.WriteString(" SET STATISTICS ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_SetStorage:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" SET STORAGE ")
			output.Builder.WriteString(strings.ToUpper(n.AlterTableCmd.Def.GetString_().GetSval()))
		case pg_query.AlterTableType_AT_SetCompression:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" SET COMPRESSION ")
			output.Builder.WriteString(n.AlterTableCmd.Def.GetString_().GetSval())
		case pg_query.AlterTableType_AT_DropIdentity:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" DROP IDENTITY")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString(" IF EXISTS")
			}
		case pg_query.AlterTableType_AT_DropExpression:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			output.Builder.WriteString(" DROP EXPRESSION")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString(" IF EXISTS")
			}
		case pg_query.AlterTableType_AT_SetLogged:
			output.Builder.WriteString("SET LOGGED")
		case pg_query.AlterTableType_AT_SetUnLogged:
			output.Builder.WriteString("SET UNLOGGED")
		case pg_query.AlterTableType_AT_SetRelOptions:
			output.Builder.WriteString("SET (")
			output.writeCommaSeparatedList(n.AlterTableCmd.Def.GetList().GetItems())
			output.Builder.WriteString(")")
		case pg_query.AlterTableType_AT_ResetRelOptions:
			output.Builder.WriteString("RESET (")
			output.writeCommaSeparatedList(n.AlterTableCmd.Def.GetList().GetItems())
			output.Builder.WriteString(")")
		case pg_query.AlterTableType_AT_SetOptions:
			output.Builder.WriteString("ALTER COLUMN ")
			output.writeAlterColumnRef(n.AlterTableCmd)
			output.Builder.WriteString(" SET (")
			output.writeCommaSeparatedList(n.AlterTableCmd.Def.GetList().GetItems())
			output.Builder.WriteString(")")
		case pg_query.AlterTableType_AT_ResetOptions:
			output.Builder.WriteString("ALTER COLUMN ")
			output.writeAlterColumnRef(n.AlterTableCmd)
			output.Builder.WriteString(" RESET (")
			output.writeCommaSeparatedList(n.AlterTableCmd.Def.GetList().GetItems())
			output.Builder.WriteString(")")
		case pg_query.AlterTableType_AT_AddInherit:
			output.Builder.WriteString("INHERIT ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_DropInherit:
			output.Builder.WriteString("NO INHERIT ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_ClusterOn:
			output.Builder.WriteString("CLUSTER ON ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_DropCluster:
			output.Builder.WriteString("SET WITHOUT CLUSTER")
		case pg_query.AlterTableType_AT_SetTableSpace:
			output.Builder.WriteString("SET TABLESPACE ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_SetAccessMethod:
			output.Builder.WriteString("SET ACCESS METHOD ")
			if n.AlterTableCmd.Name == "" {
				output.Builder.WriteString("DEFAULT")
			} else {
				output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
			}
		case pg_query.AlterTableType_AT_EnableTrig:
			output.Builder.WriteString("ENABLE TRIGGER ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_EnableAlwaysTrig:
			output.Builder.WriteString("ENABLE ALWAYS TRIGGER ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_EnableReplicaTrig:
			output.Builder.WriteString("ENABLE REPLICA TRIGGER ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_DisableTrig:
			output.Builder.WriteString("DISABLE TRIGGER ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_EnableTrigAll:
			output.Builder.WriteString("ENABLE TRIGGER ALL")
		case pg_query.AlterTableType_AT_DisableTrigAll:
			output.Builder.WriteString("DISABLE TRIGGER ALL")
		case pg_query.AlterTableType_AT_EnableTrigUser:
			output.Builder.WriteString("ENABLE TRIGGER USER")
		case pg_query.AlterTableType_AT_DisableTrigUser:
			output.Builder.WriteString("DISABLE TRIGGER USER")
		case pg_query.AlterTableType_AT_EnableRule:
			output.Builder.WriteString("ENABLE RULE ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_EnableAlwaysRule:
			output.Builder.WriteString("ENABLE ALWAYS RULE ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_EnableReplicaRule:
			output.Builder.WriteString("ENABLE REPLICA RULE ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_DisableRule:
			output.Builder.WriteString("DISABLE RULE ")
			output.Builder.WriteString(quoteIdentifier(n.AlterTableCmd.Name))
		case pg_query.AlterTableType_AT_ReplicaIdentity:
			ris := n.AlterTableCmd.Def.GetReplicaIdentityStmt()
			output.Builder.WriteString("REPLICA IDENTITY ")
			switch ris.GetIdentityType() {
			case "d":
				output.Builder.WriteString("DEFAULT")
			case "n":
				output.Builder.WriteString("NOTHING")
			case "f":
				output.Builder.WriteString("FULL")
			case "i":
				output.Builder.WriteString("USING INDEX ")
				output.Builder.WriteString(ris.Name)
			}
		default:
			warn("unsupported alter table cmd: %s", n.AlterTableCmd.Subtype.String())
		}

	case *pg_query.Node_CreateEnumStmt:
		output.Builder.WriteString("CREATE TYPE ")
		for i, name := range n.CreateEnumStmt.TypeName {
			if i > 0 {
				output.Builder.WriteString(".")
			}
			output.Builder.WriteString(name.GetString_().GetSval())
		}
		output.Builder.WriteString(" AS ENUM (\n")
		for i, val := range n.CreateEnumStmt.Vals {
			output.Builder.WriteString("\t'")
			output.Builder.WriteString(val.GetString_().GetSval())
			output.Builder.WriteString("'")
			if i < len(n.CreateEnumStmt.Vals)-1 {
				output.Builder.WriteString(",")
			}
			output.Builder.WriteString("\n")
		}
		output.Builder.WriteString(")")

	case *pg_query.Node_CompositeTypeStmt:
		output.Builder.WriteString("CREATE TYPE ")
		output.writeRangeVar(n.CompositeTypeStmt.Typevar)
		output.Builder.WriteString(" AS (\n")
		for i, col := range n.CompositeTypeStmt.Coldeflist {
			output.Builder.WriteString("\t")
			output.writeNode(col)
			if i < len(n.CompositeTypeStmt.Coldeflist)-1 {
				output.Builder.WriteString(",")
			}
			output.Builder.WriteString("\n")
		}
		output.Builder.WriteString(")")

	case *pg_query.Node_DropStmt:
		kw, ok := objectTypeKeyword(n.DropStmt.RemoveType)
		if !ok {
			warn("unsupported drop type: %s", n.DropStmt.RemoveType.String())
			if output.tryStatementFallback() {
				return
			}
		}
		output.Builder.WriteString("DROP ")
		output.Builder.WriteString(kw)
		if n.DropStmt.Concurrent {
			output.Builder.WriteString(" CONCURRENTLY")
		}
		output.Builder.WriteString(" ")
		if n.DropStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
		}
		for i, obj := range n.DropStmt.Objects {
			if i > 0 {
				output.Builder.WriteString(", ")
			}
			output.writeObjectRef(n.DropStmt.RemoveType, obj)
		}
		switch n.DropStmt.Behavior {
		case pg_query.DropBehavior_DROP_CASCADE:
			output.Builder.WriteString(" CASCADE")
		case pg_query.DropBehavior_DROP_RESTRICT:
			// default, no need to print
		}

	case *pg_query.Node_TransactionStmt:
		switch n.TransactionStmt.Kind {
		case pg_query.TransactionStmtKind_TRANS_STMT_BEGIN:
			output.Builder.WriteString("BEGIN")
			output.writeTransactionModes(n.TransactionStmt.Options)
		case pg_query.TransactionStmtKind_TRANS_STMT_START:
			output.Builder.WriteString("START TRANSACTION")
			output.writeTransactionModes(n.TransactionStmt.Options)
		case pg_query.TransactionStmtKind_TRANS_STMT_COMMIT:
			output.Builder.WriteString("COMMIT")
			if n.TransactionStmt.Chain {
				output.Builder.WriteString(" AND CHAIN")
			}
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK:
			output.Builder.WriteString("ROLLBACK")
			if n.TransactionStmt.Chain {
				output.Builder.WriteString(" AND CHAIN")
			}
		case pg_query.TransactionStmtKind_TRANS_STMT_SAVEPOINT:
			output.Builder.WriteString("SAVEPOINT ")
			output.Builder.WriteString(quoteIdentifier(n.TransactionStmt.SavepointName))
		case pg_query.TransactionStmtKind_TRANS_STMT_RELEASE:
			output.Builder.WriteString("RELEASE SAVEPOINT ")
			output.Builder.WriteString(quoteIdentifier(n.TransactionStmt.SavepointName))
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO:
			output.Builder.WriteString("ROLLBACK TO SAVEPOINT ")
			output.Builder.WriteString(quoteIdentifier(n.TransactionStmt.SavepointName))
		case pg_query.TransactionStmtKind_TRANS_STMT_PREPARE:
			output.Builder.WriteString("PREPARE TRANSACTION '")
			output.Builder.WriteString(strings.ReplaceAll(n.TransactionStmt.Gid, "'", "''"))
			output.Builder.WriteString("'")
		case pg_query.TransactionStmtKind_TRANS_STMT_COMMIT_PREPARED:
			output.Builder.WriteString("COMMIT PREPARED '")
			output.Builder.WriteString(strings.ReplaceAll(n.TransactionStmt.Gid, "'", "''"))
			output.Builder.WriteString("'")
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK_PREPARED:
			output.Builder.WriteString("ROLLBACK PREPARED '")
			output.Builder.WriteString(strings.ReplaceAll(n.TransactionStmt.Gid, "'", "''"))
			output.Builder.WriteString("'")
		default:
			warn("unsupported transaction kind: %s", n.TransactionStmt.Kind.String())
			if output.tryStatementFallback() {
				return
			}
		}

	case *pg_query.Node_DefElem:
		output.Builder.WriteString(n.DefElem.Defname)
		if n.DefElem.Arg != nil {
			output.Builder.WriteString(" = ")
			output.writeNode(n.DefElem.Arg)
		}

	case *pg_query.Node_DoStmt:
		output.Builder.WriteString("DO")
		var lang, asBody string
		for _, arg := range n.DoStmt.Args {
			de := arg.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "language":
				lang = de.Arg.GetString_().GetSval()
			case "as":
				if s := de.Arg.GetString_(); s != nil {
					asBody = s.GetSval()
				} else if l := de.Arg.GetList(); l != nil && len(l.Items) > 0 {
					asBody = l.Items[0].GetString_().GetSval()
				}
			}
		}
		if lang != "" && !strings.EqualFold(lang, "plpgsql") {
			output.Builder.WriteString(" LANGUAGE ")
			output.Builder.WriteString(lang)
		}
		if asBody != "" {
			if strings.EqualFold(lang, "plpgsql") || lang == "" {
				output.writeDollarQuotedBody(" ", func(p *Printer) {
					p.formatPLpgSQLBody(asBody, 0)
				})
			} else if strings.EqualFold(lang, "sql") {
				output.writeDollarQuotedBody(" ", func(p *Printer) {
					p.formatSQLBody(asBody, 1)
				})
			} else {
				output.writeDollarQuotedBody(" ", func(p *Printer) {
					p.Builder.WriteString("\n")
					p.Builder.WriteString(strings.Trim(asBody, "\n"))
				})
			}
		}

	case *pg_query.Node_CreateFunctionStmt:
		// SQL-standard bodies (BEGIN ATOMIC ... END / RETURN expr) are not
		// supported by the printer yet; emitting the function without its
		// body would be invalid.
		if n.CreateFunctionStmt.SqlBody != nil && output.tryStatementFallback() {
			return
		}
		output.Builder.WriteString("CREATE ")
		if n.CreateFunctionStmt.Replace {
			output.Builder.WriteString("OR REPLACE ")
		}
		if n.CreateFunctionStmt.IsProcedure {
			output.Builder.WriteString("PROCEDURE ")
		} else {
			output.Builder.WriteString("FUNCTION ")
		}
		output.writeFuncName(n.CreateFunctionStmt.Funcname)
		output.Builder.WriteString("(")
		output.writeFunctionParams(n.CreateFunctionStmt.Parameters)
		output.Builder.WriteString(")")
		if n.CreateFunctionStmt.ReturnType != nil {
			// Check if this is RETURNS TABLE
			var tableParams []*pg_query.Node
			for _, p := range n.CreateFunctionStmt.Parameters {
				fp := p.GetFunctionParameter()
				if fp != nil && fp.Mode == pg_query.FunctionParameterMode_FUNC_PARAM_TABLE {
					tableParams = append(tableParams, p)
				}
			}
			if len(tableParams) > 0 {
				output.Builder.WriteString("\nRETURNS TABLE (")
				for i, p := range tableParams {
					fp := p.GetFunctionParameter()
					output.Builder.WriteString("\n\t")
					output.Builder.WriteString(fp.Name)
					output.Builder.WriteString(" ")
					output.writeTypeName(fp.ArgType)
					if i != len(tableParams)-1 {
						output.Builder.WriteString(",")
					}
				}
				output.Builder.WriteString("\n)")
			} else {
				output.Builder.WriteString("\nRETURNS ")
				output.writeTypeName(n.CreateFunctionStmt.ReturnType)
			}
		}
		// Collect options by name for ordered output
		var lang, asBody string
		var otherOpts []string
		for _, opt := range n.CreateFunctionStmt.Options {
			de := opt.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "language":
				lang = de.Arg.GetString_().GetSval()
			case "as":
				if l := de.Arg.GetList(); l != nil && len(l.Items) > 0 {
					asBody = l.Items[0].GetString_().GetSval()
				}
			case "volatility":
				otherOpts = append(otherOpts, strings.ToUpper(de.Arg.GetString_().GetSval()))
			case "strict":
				if de.Arg.GetBoolean().GetBoolval() {
					otherOpts = append(otherOpts, "STRICT")
				} else {
					otherOpts = append(otherOpts, "CALLED ON NULL INPUT")
				}
			case "security":
				if de.Arg.GetBoolean().GetBoolval() {
					otherOpts = append(otherOpts, "SECURITY DEFINER")
				} else {
					otherOpts = append(otherOpts, "SECURITY INVOKER")
				}
			case "parallel":
				otherOpts = append(otherOpts, "PARALLEL "+strings.ToUpper(de.Arg.GetString_().GetSval()))
			case "set":
				b := &strings.Builder{}
				tmp := &Printer{Builder: b}
				tmp.writeNode(de.Arg)
				otherOpts = append(otherOpts, b.String())
			}
		}
		if lang != "" {
			output.Builder.WriteString("\nLANGUAGE ")
			output.Builder.WriteString(lang)
		}
		for _, o := range otherOpts {
			output.Builder.WriteString("\n")
			output.Builder.WriteString(o)
		}
		if asBody != "" {
			if strings.EqualFold(lang, "sql") {
				output.writeDollarQuotedBody("\nAS ", func(p *Printer) {
					p.formatSQLBody(asBody, 1)
				})
			} else if strings.EqualFold(lang, "plpgsql") {
				output.writeDollarQuotedBody("\nAS ", func(p *Printer) {
					p.formatPLpgSQLBody(asBody, 0)
				})
			} else {
				output.writeDollarQuotedBody("\nAS ", func(p *Printer) {
					p.Builder.WriteString("\n")
					p.Builder.WriteString(strings.Trim(asBody, "\n"))
				})
			}
		}

	case *pg_query.Node_FunctionParameter:
		switch n.FunctionParameter.Mode {
		case pg_query.FunctionParameterMode_FUNC_PARAM_IN:
			// IN is default, don't print
		case pg_query.FunctionParameterMode_FUNC_PARAM_OUT:
			output.Builder.WriteString("OUT ")
		case pg_query.FunctionParameterMode_FUNC_PARAM_INOUT:
			output.Builder.WriteString("INOUT ")
		case pg_query.FunctionParameterMode_FUNC_PARAM_VARIADIC:
			output.Builder.WriteString("VARIADIC ")
		}
		if n.FunctionParameter.Name != "" {
			output.Builder.WriteString(n.FunctionParameter.Name)
			output.Builder.WriteString(" ")
		}
		output.writeTypeName(n.FunctionParameter.ArgType)
		if n.FunctionParameter.Defexpr != nil {
			output.Builder.WriteString(" DEFAULT ")
			output.writeNode(n.FunctionParameter.Defexpr)
		}

	case *pg_query.Node_ObjectWithArgs:
		output.writeListWithSeparator(n.ObjectWithArgs.Objname, ".")
		if !n.ObjectWithArgs.ArgsUnspecified {
			output.Builder.WriteString("(")
			for i, arg := range n.ObjectWithArgs.Objargs {
				if tn := arg.GetTypeName(); tn != nil {
					output.writeTypeName(tn)
				} else {
					output.writeNode(arg)
				}
				if i != len(n.ObjectWithArgs.Objargs)-1 {
					output.Builder.WriteString(", ")
				}
			}
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_LockingClause:
		switch n.LockingClause.Strength {
		case pg_query.LockClauseStrength_LCS_FORKEYSHARE:
			output.Builder.WriteString("FOR KEY SHARE")
		case pg_query.LockClauseStrength_LCS_FORSHARE:
			output.Builder.WriteString("FOR SHARE")
		case pg_query.LockClauseStrength_LCS_FORNOKEYUPDATE:
			output.Builder.WriteString("FOR NO KEY UPDATE")
		case pg_query.LockClauseStrength_LCS_FORUPDATE:
			output.Builder.WriteString("FOR UPDATE")
		}
		if len(n.LockingClause.LockedRels) > 0 {
			output.Builder.WriteString(" OF ")
			output.writeCommaSeparatedList(n.LockingClause.LockedRels)
		}
		switch n.LockingClause.WaitPolicy {
		case pg_query.LockWaitPolicy_LockWaitSkip:
			output.Builder.WriteString(" SKIP LOCKED")
		case pg_query.LockWaitPolicy_LockWaitError:
			output.Builder.WriteString(" NOWAIT")
		case pg_query.LockWaitPolicy_LockWaitBlock:
			// default, no keyword needed
		}

	case *pg_query.Node_WindowDef:
		output.writeWindowDef(n.WindowDef)

	case *pg_query.Node_CaseExpr:
		output.Builder.WriteString("CASE")
		if n.CaseExpr.Arg != nil {
			output.Builder.WriteString(" ")
			output.writeNode(n.CaseExpr.Arg)
		}
		for _, w := range n.CaseExpr.Args {
			cw := w.GetCaseWhen()
			if cw == nil {
				continue
			}
			output.Builder.WriteString(" WHEN ")
			output.writeNode(cw.Expr)
			output.Builder.WriteString(" THEN ")
			output.writeNode(cw.Result)
		}
		if n.CaseExpr.Defresult != nil {
			output.Builder.WriteString(" ELSE ")
			output.writeNode(n.CaseExpr.Defresult)
		}
		output.Builder.WriteString(" END")

	case *pg_query.Node_MinMaxExpr:
		switch n.MinMaxExpr.Op {
		case pg_query.MinMaxOp_IS_GREATEST:
			output.Builder.WriteString("GREATEST(")
		case pg_query.MinMaxOp_IS_LEAST:
			output.Builder.WriteString("LEAST(")
		default:
			warn("unsupported min/max op: %s", n.MinMaxExpr.Op.String())
			output.Builder.WriteString("(")
		}
		output.writeCommaSeparatedList(n.MinMaxExpr.Args)
		output.Builder.WriteString(")")

	case *pg_query.Node_SqlvalueFunction:
		switch n.SqlvalueFunction.Op {
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_DATE:
			output.Builder.WriteString("CURRENT_DATE")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIME:
			output.Builder.WriteString("CURRENT_TIME")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIME_N:
			output.Builder.WriteString("CURRENT_TIME(")
			output.Builder.WriteString(strconv.Itoa(int(n.SqlvalueFunction.Typmod)))
			output.Builder.WriteString(")")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP:
			output.Builder.WriteString("CURRENT_TIMESTAMP")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP_N:
			output.Builder.WriteString("CURRENT_TIMESTAMP(")
			output.Builder.WriteString(strconv.Itoa(int(n.SqlvalueFunction.Typmod)))
			output.Builder.WriteString(")")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIME:
			output.Builder.WriteString("LOCALTIME")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIME_N:
			output.Builder.WriteString("LOCALTIME(")
			output.Builder.WriteString(strconv.Itoa(int(n.SqlvalueFunction.Typmod)))
			output.Builder.WriteString(")")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP:
			output.Builder.WriteString("LOCALTIMESTAMP")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N:
			output.Builder.WriteString("LOCALTIMESTAMP(")
			output.Builder.WriteString(strconv.Itoa(int(n.SqlvalueFunction.Typmod)))
			output.Builder.WriteString(")")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_ROLE:
			output.Builder.WriteString("CURRENT_ROLE")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_USER:
			output.Builder.WriteString("CURRENT_USER")
		case pg_query.SQLValueFunctionOp_SVFOP_USER:
			output.Builder.WriteString("USER")
		case pg_query.SQLValueFunctionOp_SVFOP_SESSION_USER:
			output.Builder.WriteString("SESSION_USER")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_CATALOG:
			output.Builder.WriteString("CURRENT_CATALOG")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_SCHEMA:
			output.Builder.WriteString("CURRENT_SCHEMA")
		default:
			warn("unsupported sqlvalue function: %s", n.SqlvalueFunction.Op.String())
		}

	case *pg_query.Node_GroupingFunc:
		output.Builder.WriteString("GROUPING(")
		output.writeCommaSeparatedList(n.GroupingFunc.Args)
		output.Builder.WriteString(")")

	case *pg_query.Node_SetToDefault:
		output.Builder.WriteString("DEFAULT")

	case *pg_query.Node_GroupingSet:
		switch n.GroupingSet.Kind {
		case pg_query.GroupingSetKind_GROUPING_SET_ROLLUP:
			output.Builder.WriteString("ROLLUP(")
			output.writeCommaSeparatedList(n.GroupingSet.Content)
			output.Builder.WriteString(")")
		case pg_query.GroupingSetKind_GROUPING_SET_CUBE:
			output.Builder.WriteString("CUBE(")
			output.writeCommaSeparatedList(n.GroupingSet.Content)
			output.Builder.WriteString(")")
		case pg_query.GroupingSetKind_GROUPING_SET_SETS:
			output.Builder.WriteString("GROUPING SETS (")
			output.writeCommaSeparatedList(n.GroupingSet.Content)
			output.Builder.WriteString(")")
		case pg_query.GroupingSetKind_GROUPING_SET_EMPTY:
			output.Builder.WriteString("()")
		default:
			output.writeCommaSeparatedList(n.GroupingSet.Content)
		}

	case *pg_query.Node_TypeName:
		output.writeTypeName(n.TypeName)

	case *pg_query.Node_ViewStmt:
		output.Builder.WriteString("CREATE ")
		if n.ViewStmt.Replace {
			output.Builder.WriteString("OR REPLACE ")
		}
		output.Builder.WriteString("VIEW ")
		output.writeRangeVar(n.ViewStmt.View)
		output.Builder.WriteString(" AS\n")
		output.writeNode(n.ViewStmt.Query)

	case *pg_query.Node_CreateTableAsStmt:
		output.Builder.WriteString("CREATE ")
		if n.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
			output.Builder.WriteString("MATERIALIZED VIEW ")
		} else {
			output.Builder.WriteString("TABLE ")
		}
		if n.CreateTableAsStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		if n.CreateTableAsStmt.Into != nil {
			output.writeRangeVar(n.CreateTableAsStmt.Into.Rel)
		}
		output.Builder.WriteString(" AS\n")
		output.writeNode(n.CreateTableAsStmt.Query)

	case *pg_query.Node_CreateSchemaStmt:
		output.Builder.WriteString("CREATE SCHEMA ")
		if n.CreateSchemaStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		if n.CreateSchemaStmt.Schemaname != "" {
			output.Builder.WriteString(quoteIdentifier(n.CreateSchemaStmt.Schemaname))
		}
		if n.CreateSchemaStmt.Authrole != nil {
			if n.CreateSchemaStmt.Schemaname != "" {
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("AUTHORIZATION ")
			output.writeRoleSpec(n.CreateSchemaStmt.Authrole)
		}
		// Schema elements (CREATE TABLE/VIEW/... nested in CREATE SCHEMA).
		for _, elt := range n.CreateSchemaStmt.SchemaElts {
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(elt)
			output.indent--
		}

	case *pg_query.Node_CreateSeqStmt:
		output.Builder.WriteString("CREATE SEQUENCE ")
		if n.CreateSeqStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		output.writeRangeVar(n.CreateSeqStmt.Sequence)
		for _, opt := range n.CreateSeqStmt.Options {
			de := opt.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "as":
				output.Builder.WriteString(" AS ")
				if tn := de.Arg.GetTypeName(); tn != nil {
					output.writeTypeName(tn)
				} else {
					output.writeNode(de.Arg)
				}
			case "owned_by":
				output.Builder.WriteString(" OWNED BY ")
				if l := de.Arg.GetList(); l != nil {
					output.writeListWithSeparator(l.Items, ".")
				} else {
					output.writeNode(de.Arg)
				}
			case "start":
				output.Builder.WriteString(" START WITH ")
				output.writeNode(de.Arg)
			case "increment":
				output.Builder.WriteString(" INCREMENT BY ")
				output.writeNode(de.Arg)
			case "minvalue":
				if de.Arg != nil {
					output.Builder.WriteString(" MINVALUE ")
					output.writeNode(de.Arg)
				} else {
					output.Builder.WriteString(" NO MINVALUE")
				}
			case "maxvalue":
				if de.Arg != nil {
					output.Builder.WriteString(" MAXVALUE ")
					output.writeNode(de.Arg)
				} else {
					output.Builder.WriteString(" NO MAXVALUE")
				}
			case "cache":
				output.Builder.WriteString(" CACHE ")
				output.writeNode(de.Arg)
			case "cycle":
				if de.Arg != nil && de.Arg.GetBoolean().GetBoolval() {
					output.Builder.WriteString(" CYCLE")
				} else {
					output.Builder.WriteString(" NO CYCLE")
				}
			}
		}

	case *pg_query.Node_CreateExtensionStmt:
		output.Builder.WriteString("CREATE EXTENSION ")
		if n.CreateExtensionStmt.IfNotExists {
			output.Builder.WriteString("IF NOT EXISTS ")
		}
		output.Builder.WriteString(n.CreateExtensionStmt.Extname)

	case *pg_query.Node_GrantStmt:
		objKw, objOk := grantObjectKeyword(n.GrantStmt.Targtype, n.GrantStmt.Objtype)
		if !objOk {
			warn("unsupported grant object type: %s", n.GrantStmt.Objtype.String())
			if output.tryStatementFallback() {
				return
			}
		}
		if n.GrantStmt.IsGrant {
			output.Builder.WriteString("GRANT ")
		} else {
			output.Builder.WriteString("REVOKE ")
			if n.GrantStmt.GrantOption {
				output.Builder.WriteString("GRANT OPTION FOR ")
			}
		}
		if len(n.GrantStmt.Privileges) == 0 {
			output.Builder.WriteString("ALL")
		} else {
			for i, p := range n.GrantStmt.Privileges {
				if i > 0 {
					output.Builder.WriteString(", ")
				}
				ap := p.GetAccessPriv()
				if ap == nil {
					continue
				}
				// An empty priv_name means ALL (used with column lists).
				if ap.PrivName == "" {
					output.Builder.WriteString("ALL")
				} else {
					output.Builder.WriteString(strings.ToUpper(ap.PrivName))
				}
				if len(ap.Cols) > 0 {
					output.Builder.WriteString(" (")
					output.writeCommaSeparatedList(ap.Cols)
					output.Builder.WriteString(")")
				}
			}
		}
		output.Builder.WriteString(" ON ")
		output.Builder.WriteString(objKw)
		for i, obj := range n.GrantStmt.Objects {
			if i > 0 {
				output.Builder.WriteString(", ")
			}
			output.writeNode(obj)
		}
		if n.GrantStmt.IsGrant {
			output.Builder.WriteString(" TO ")
		} else {
			output.Builder.WriteString(" FROM ")
		}
		for i, g := range n.GrantStmt.Grantees {
			if i > 0 {
				output.Builder.WriteString(", ")
			}
			output.writeRoleSpec(g.GetRoleSpec())
		}
		if n.GrantStmt.IsGrant && n.GrantStmt.GrantOption {
			output.Builder.WriteString(" WITH GRANT OPTION")
		}
		if !n.GrantStmt.IsGrant && n.GrantStmt.Behavior == pg_query.DropBehavior_DROP_CASCADE {
			output.Builder.WriteString(" CASCADE")
		}

	case *pg_query.Node_CommentStmt:
		kw, ok := objectTypeKeyword(n.CommentStmt.Objtype)
		if !ok {
			warn("unsupported comment object type: %s", n.CommentStmt.Objtype.String())
			if output.tryStatementFallback() {
				return
			}
		}
		output.Builder.WriteString("COMMENT ON ")
		output.Builder.WriteString(kw)
		output.Builder.WriteString(" ")
		output.writeObjectRef(n.CommentStmt.Objtype, n.CommentStmt.Object)
		if n.CommentStmt.Comment == "" {
			// An empty comment means removal; deparse emits NULL as well.
			output.Builder.WriteString(" IS NULL")
		} else {
			output.Builder.WriteString(" IS '")
			output.Builder.WriteString(strings.ReplaceAll(n.CommentStmt.Comment, "'", "''"))
			output.Builder.WriteString("'")
		}

	case *pg_query.Node_TruncateStmt:
		output.Builder.WriteString("TRUNCATE TABLE ")
		for i, rel := range n.TruncateStmt.Relations {
			output.writeNode(rel)
			if i != len(n.TruncateStmt.Relations)-1 {
				output.Builder.WriteString(", ")
			}
		}
		if n.TruncateStmt.RestartSeqs {
			output.Builder.WriteString(" RESTART IDENTITY")
		}
		switch n.TruncateStmt.Behavior {
		case pg_query.DropBehavior_DROP_CASCADE:
			output.Builder.WriteString(" CASCADE")
		case pg_query.DropBehavior_DROP_RESTRICT:
			// default
		}

	case *pg_query.Node_ExplainStmt:
		output.Builder.WriteString("EXPLAIN")
		if len(n.ExplainStmt.Options) > 0 {
			// Use shorthand for single ANALYZE or VERBOSE; parenthesized form otherwise
			useShorthand := len(n.ExplainStmt.Options) == 1
			if useShorthand {
				de := n.ExplainStmt.Options[0].GetDefElem()
				useShorthand = de != nil && (de.Defname == "analyze" || de.Defname == "verbose") && de.Arg == nil
			}
			if useShorthand {
				output.Builder.WriteString(" ")
				output.Builder.WriteString(strings.ToUpper(n.ExplainStmt.Options[0].GetDefElem().Defname))
			} else {
				output.Builder.WriteString(" (")
				for i, opt := range n.ExplainStmt.Options {
					de := opt.GetDefElem()
					if de == nil {
						continue
					}
					output.Builder.WriteString(strings.ToUpper(de.Defname))
					if de.Arg != nil {
						output.Builder.WriteString(" ")
						if s := de.Arg.GetString_(); s != nil {
							output.Builder.WriteString(s.GetSval())
						} else {
							output.writeNode(de.Arg)
						}
					}
					if i != len(n.ExplainStmt.Options)-1 {
						output.Builder.WriteString(", ")
					}
				}
				output.Builder.WriteString(")")
			}
		}
		output.Builder.WriteString("\n")
		output.writeNode(n.ExplainStmt.Query)

	case *pg_query.Node_CopyStmt:
		output.Builder.WriteString("COPY ")
		if n.CopyStmt.Relation != nil {
			output.writeRangeVar(n.CopyStmt.Relation)
		} else if n.CopyStmt.Query != nil {
			// COPY (select/insert/update/delete/merge ... ) TO ...
			output.Builder.WriteString("(")
			output.writeNode(n.CopyStmt.Query)
			output.Builder.WriteString(")")
		}
		if len(n.CopyStmt.Attlist) > 0 {
			output.Builder.WriteString(" (")
			output.writeQuotedIdentifierList(n.CopyStmt.Attlist)
			output.Builder.WriteString(")")
		}
		if n.CopyStmt.IsFrom {
			output.Builder.WriteString(" FROM ")
		} else {
			output.Builder.WriteString(" TO ")
		}
		if n.CopyStmt.Filename != "" {
			output.Builder.WriteString("'")
			output.Builder.WriteString(strings.ReplaceAll(n.CopyStmt.Filename, "'", "''"))
			output.Builder.WriteString("'")
		} else if n.CopyStmt.IsFrom {
			output.Builder.WriteString("STDIN")
		} else {
			output.Builder.WriteString("STDOUT")
		}
		if len(n.CopyStmt.Options) > 0 {
			output.Builder.WriteString(" WITH (")
			for i, opt := range n.CopyStmt.Options {
				de := opt.GetDefElem()
				if de == nil {
					continue
				}
				output.Builder.WriteString(strings.ToUpper(de.Defname))
				if de.Arg != nil {
					output.Builder.WriteString(" ")
					if s := de.Arg.GetString_(); s != nil {
						// These options take string literals; the rest take
						// identifiers or booleans stored as strings.
						switch de.Defname {
						case "delimiter", "null", "quote", "escape", "encoding", "default":
							output.Builder.WriteString("'")
							output.Builder.WriteString(strings.ReplaceAll(s.GetSval(), "'", "''"))
							output.Builder.WriteString("'")
						default:
							output.Builder.WriteString(s.GetSval())
						}
					} else if l := de.Arg.GetList(); l != nil {
						// Column lists: FORCE_QUOTE (a, b), FORCE_NULL (c), ...
						output.Builder.WriteString("(")
						output.writeQuotedIdentifierList(l.Items)
						output.Builder.WriteString(")")
					} else if de.Arg.GetAStar() != nil {
						output.Builder.WriteString("*")
					} else {
						output.writeNode(de.Arg)
					}
				}
				if i != len(n.CopyStmt.Options)-1 {
					output.Builder.WriteString(", ")
				}
			}
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_ListenStmt:
		output.Builder.WriteString("LISTEN ")
		output.Builder.WriteString(n.ListenStmt.Conditionname)

	case *pg_query.Node_NotifyStmt:
		output.Builder.WriteString("NOTIFY ")
		output.Builder.WriteString(n.NotifyStmt.Conditionname)
		if n.NotifyStmt.Payload != "" {
			output.Builder.WriteString(", '")
			output.Builder.WriteString(strings.ReplaceAll(n.NotifyStmt.Payload, "'", "''"))
			output.Builder.WriteString("'")
		}

	case *pg_query.Node_UnlistenStmt:
		output.Builder.WriteString("UNLISTEN ")
		if n.UnlistenStmt.Conditionname == "" {
			output.Builder.WriteString("*")
		} else {
			output.Builder.WriteString(quoteIdentifier(n.UnlistenStmt.Conditionname))
		}

	case *pg_query.Node_VariableSetStmt:
		switch n.VariableSetStmt.Kind {
		case pg_query.VariableSetKind_VAR_SET_VALUE:
			output.Builder.WriteString("SET ")
			output.writeGUCName(n.VariableSetStmt.Name)
			output.Builder.WriteString(" TO ")
			output.writeCommaSeparatedList(n.VariableSetStmt.Args)
		case pg_query.VariableSetKind_VAR_SET_DEFAULT:
			output.Builder.WriteString("SET ")
			output.writeGUCName(n.VariableSetStmt.Name)
			output.Builder.WriteString(" TO DEFAULT")
		case pg_query.VariableSetKind_VAR_SET_CURRENT:
			output.Builder.WriteString("SET ")
			output.writeGUCName(n.VariableSetStmt.Name)
			output.Builder.WriteString(" FROM CURRENT")
		case pg_query.VariableSetKind_VAR_RESET:
			output.Builder.WriteString("RESET ")
			output.writeGUCName(n.VariableSetStmt.Name)
		case pg_query.VariableSetKind_VAR_RESET_ALL:
			output.Builder.WriteString("RESET ALL")
		case pg_query.VariableSetKind_VAR_SET_MULTI:
			// SET TRANSACTION ... / SET SESSION CHARACTERISTICS AS TRANSACTION ...
			switch n.VariableSetStmt.Name {
			case "TRANSACTION":
				output.Builder.WriteString("SET TRANSACTION")
				output.writeTransactionModes(n.VariableSetStmt.Args)
			case "SESSION CHARACTERISTICS":
				output.Builder.WriteString("SET SESSION CHARACTERISTICS AS TRANSACTION")
				output.writeTransactionModes(n.VariableSetStmt.Args)
			case "TRANSACTION SNAPSHOT":
				output.Builder.WriteString("SET TRANSACTION SNAPSHOT ")
				if len(n.VariableSetStmt.Args) > 0 {
					output.writeNode(n.VariableSetStmt.Args[0])
				}
			default:
				warn("unsupported SET MULTI name: %s", n.VariableSetStmt.Name)
				if output.tryStatementFallback() {
					return
				}
			}
		default:
			warn("unsupported variable set kind: %s", n.VariableSetStmt.Kind.String())
			if output.tryStatementFallback() {
				return
			}
		}

	case *pg_query.Node_VariableShowStmt:
		output.Builder.WriteString("SHOW ")
		output.writeGUCName(n.VariableShowStmt.Name)

	case *pg_query.Node_PrepareStmt:
		output.Builder.WriteString("PREPARE ")
		output.Builder.WriteString(n.PrepareStmt.Name)
		if len(n.PrepareStmt.Argtypes) > 0 {
			output.Builder.WriteString(" (")
			for i, at := range n.PrepareStmt.Argtypes {
				if tn := at.GetTypeName(); tn != nil {
					output.writeTypeName(tn)
				} else {
					output.writeNode(at)
				}
				if i != len(n.PrepareStmt.Argtypes)-1 {
					output.Builder.WriteString(", ")
				}
			}
			output.Builder.WriteString(")")
		}
		output.Builder.WriteString(" AS\n")
		output.writeNode(n.PrepareStmt.Query)

	case *pg_query.Node_ExecuteStmt:
		output.Builder.WriteString("EXECUTE ")
		output.Builder.WriteString(n.ExecuteStmt.Name)
		if len(n.ExecuteStmt.Params) > 0 {
			output.Builder.WriteString("(")
			output.writeCommaSeparatedList(n.ExecuteStmt.Params)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_DeallocateStmt:
		output.Builder.WriteString("DEALLOCATE ")
		if n.DeallocateStmt.Name == "" {
			output.Builder.WriteString("ALL")
		} else {
			output.Builder.WriteString(quoteIdentifier(n.DeallocateStmt.Name))
		}

	case *pg_query.Node_VacuumStmt:
		if n.VacuumStmt.IsVacuumcmd {
			output.Builder.WriteString("VACUUM")
		} else {
			output.Builder.WriteString("ANALYZE")
		}
		if len(n.VacuumStmt.Options) > 0 {
			output.Builder.WriteString(" (")
			for i, opt := range n.VacuumStmt.Options {
				de := opt.GetDefElem()
				if de == nil {
					continue
				}
				if i > 0 {
					output.Builder.WriteString(", ")
				}
				output.Builder.WriteString(strings.ToUpper(de.Defname))
				if de.Arg != nil {
					output.Builder.WriteString(" ")
					if s := de.Arg.GetString_(); s != nil {
						if isBareWord(s.GetSval()) {
							output.Builder.WriteString(s.GetSval())
						} else {
							output.Builder.WriteString("'")
							output.Builder.WriteString(strings.ReplaceAll(s.GetSval(), "'", "''"))
							output.Builder.WriteString("'")
						}
					} else {
						output.writeNode(de.Arg)
					}
				}
			}
			output.Builder.WriteString(")")
		}
		for i, rel := range n.VacuumStmt.Rels {
			if i > 0 {
				output.Builder.WriteString(",")
			}
			output.Builder.WriteString(" ")
			output.writeNode(rel)
		}

	case *pg_query.Node_VacuumRelation:
		if n.VacuumRelation.Relation != nil {
			output.writeRangeVar(n.VacuumRelation.Relation)
		}
		if len(n.VacuumRelation.VaCols) > 0 {
			output.Builder.WriteString(" (")
			output.writeQuotedIdentifierList(n.VacuumRelation.VaCols)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_CreateTrigStmt:
		output.Builder.WriteString("CREATE ")
		if n.CreateTrigStmt.Isconstraint {
			output.Builder.WriteString("CONSTRAINT ")
		}
		output.Builder.WriteString("TRIGGER ")
		output.Builder.WriteString(n.CreateTrigStmt.Trigname)
		// Timing: BEFORE=2, AFTER=0 (default), INSTEAD OF=64
		switch {
		case n.CreateTrigStmt.Timing&64 != 0:
			output.Builder.WriteString("\nINSTEAD OF ")
		case n.CreateTrigStmt.Timing&2 != 0:
			output.Builder.WriteString("\nBEFORE ")
		default:
			output.Builder.WriteString("\nAFTER ")
		}
		// Events: INSERT=4, DELETE=8, UPDATE=16, TRUNCATE=32
		events := []string{}
		if n.CreateTrigStmt.Events&4 != 0 {
			events = append(events, "INSERT")
		}
		if n.CreateTrigStmt.Events&8 != 0 {
			events = append(events, "DELETE")
		}
		if n.CreateTrigStmt.Events&16 != 0 {
			events = append(events, "UPDATE")
		}
		if n.CreateTrigStmt.Events&32 != 0 {
			events = append(events, "TRUNCATE")
		}
		output.Builder.WriteString(strings.Join(events, " OR "))
		output.Builder.WriteString(" ON ")
		output.writeRangeVar(n.CreateTrigStmt.Relation)
		if n.CreateTrigStmt.Row {
			output.Builder.WriteString("\nFOR EACH ROW")
		} else {
			output.Builder.WriteString("\nFOR EACH STATEMENT")
		}
		if n.CreateTrigStmt.WhenClause != nil {
			output.Builder.WriteString("\nWHEN (")
			output.writeNode(n.CreateTrigStmt.WhenClause)
			output.Builder.WriteString(")")
		}
		output.Builder.WriteString("\nEXECUTE FUNCTION ")
		output.writeFuncName(n.CreateTrigStmt.Funcname)
		output.Builder.WriteString("(")
		for i, a := range n.CreateTrigStmt.Args {
			if i > 0 {
				output.Builder.WriteString(", ")
			}
			output.Builder.WriteString("'")
			output.Builder.WriteString(strings.ReplaceAll(a.GetString_().GetSval(), "'", "''"))
			output.Builder.WriteString("'")
		}
		output.Builder.WriteString(")")

	case *pg_query.Node_CreatePolicyStmt:
		output.Builder.WriteString("CREATE POLICY ")
		output.Builder.WriteString(n.CreatePolicyStmt.PolicyName)
		output.Builder.WriteString("\nON ")
		output.writeRangeVar(n.CreatePolicyStmt.Table)
		if !n.CreatePolicyStmt.Permissive {
			output.Builder.WriteString("\nAS RESTRICTIVE")
		}
		if n.CreatePolicyStmt.CmdName != "" && n.CreatePolicyStmt.CmdName != "all" {
			output.Builder.WriteString("\nFOR ")
			output.Builder.WriteString(strings.ToUpper(n.CreatePolicyStmt.CmdName))
		}
		if len(n.CreatePolicyStmt.Roles) > 0 {
			output.Builder.WriteString("\nTO ")
			for i, role := range n.CreatePolicyStmt.Roles {
				if i > 0 {
					output.Builder.WriteString(", ")
				}
				rs := role.GetRoleSpec()
				if rs != nil {
					switch rs.Roletype {
					case pg_query.RoleSpecType_ROLESPEC_PUBLIC:
						output.Builder.WriteString("PUBLIC")
					case pg_query.RoleSpecType_ROLESPEC_CURRENT_USER:
						output.Builder.WriteString("CURRENT_USER")
					case pg_query.RoleSpecType_ROLESPEC_SESSION_USER:
						output.Builder.WriteString("SESSION_USER")
					case pg_query.RoleSpecType_ROLESPEC_CURRENT_ROLE:
						output.Builder.WriteString("CURRENT_ROLE")
					default:
						output.Builder.WriteString(rs.Rolename)
					}
				}
			}
		}
		if n.CreatePolicyStmt.Qual != nil {
			output.Builder.WriteString("\nUSING (")
			output.writeNode(n.CreatePolicyStmt.Qual)
			output.Builder.WriteString(")")
		}
		if n.CreatePolicyStmt.WithCheck != nil {
			output.Builder.WriteString("\nWITH CHECK (")
			output.writeNode(n.CreatePolicyStmt.WithCheck)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_CreateDomainStmt:
		output.Builder.WriteString("CREATE DOMAIN ")
		output.writeListWithSeparator(n.CreateDomainStmt.Domainname, ".")
		output.Builder.WriteString(" AS ")
		output.writeTypeName(n.CreateDomainStmt.TypeName)
		for _, c := range n.CreateDomainStmt.Constraints {
			output.Builder.WriteString(" ")
			output.writeNode(c)
		}

	case *pg_query.Node_AlterSeqStmt:
		output.Builder.WriteString("ALTER SEQUENCE ")
		if n.AlterSeqStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
		}
		output.writeRangeVar(n.AlterSeqStmt.Sequence)
		for _, opt := range n.AlterSeqStmt.Options {
			de := opt.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
			case "as":
				output.Builder.WriteString(" AS ")
				if tn := de.Arg.GetTypeName(); tn != nil {
					output.writeTypeName(tn)
				} else {
					output.writeNode(de.Arg)
				}
			case "restart":
				output.Builder.WriteString(" RESTART")
				if de.Arg != nil {
					output.Builder.WriteString(" WITH ")
					output.writeNode(de.Arg)
				}
			case "start":
				output.Builder.WriteString(" START WITH ")
				output.writeNode(de.Arg)
			case "increment":
				output.Builder.WriteString(" INCREMENT BY ")
				output.writeNode(de.Arg)
			case "minvalue":
				if de.Arg != nil {
					output.Builder.WriteString(" MINVALUE ")
					output.writeNode(de.Arg)
				} else {
					output.Builder.WriteString(" NO MINVALUE")
				}
			case "maxvalue":
				if de.Arg != nil {
					output.Builder.WriteString(" MAXVALUE ")
					output.writeNode(de.Arg)
				} else {
					output.Builder.WriteString(" NO MAXVALUE")
				}
			case "cache":
				output.Builder.WriteString(" CACHE ")
				output.writeNode(de.Arg)
			case "cycle":
				if de.Arg != nil && de.Arg.GetBoolean().GetBoolval() {
					output.Builder.WriteString(" CYCLE")
				} else {
					output.Builder.WriteString(" NO CYCLE")
				}
			case "owned_by":
				output.Builder.WriteString(" OWNED BY ")
				if l := de.Arg.GetList(); l != nil {
					output.writeListWithSeparator(l.Items, ".")
				} else {
					output.writeNode(de.Arg)
				}
			}
		}

	case *pg_query.Node_ReindexStmt:
		output.Builder.WriteString("REINDEX ")
		switch n.ReindexStmt.Kind {
		case pg_query.ReindexObjectType_REINDEX_OBJECT_INDEX:
			output.Builder.WriteString("INDEX ")
		case pg_query.ReindexObjectType_REINDEX_OBJECT_TABLE:
			output.Builder.WriteString("TABLE ")
		case pg_query.ReindexObjectType_REINDEX_OBJECT_SCHEMA:
			output.Builder.WriteString("SCHEMA ")
		case pg_query.ReindexObjectType_REINDEX_OBJECT_SYSTEM:
			output.Builder.WriteString("SYSTEM ")
		case pg_query.ReindexObjectType_REINDEX_OBJECT_DATABASE:
			output.Builder.WriteString("DATABASE ")
		}
		if n.ReindexStmt.Relation != nil {
			output.writeRangeVar(n.ReindexStmt.Relation)
		} else if n.ReindexStmt.Name != "" {
			output.Builder.WriteString(n.ReindexStmt.Name)
		}

	case *pg_query.Node_ClusterStmt:
		output.Builder.WriteString("CLUSTER ")
		if n.ClusterStmt.Relation != nil {
			output.writeRangeVar(n.ClusterStmt.Relation)
		}
		if n.ClusterStmt.Indexname != "" {
			output.Builder.WriteString(" USING ")
			output.Builder.WriteString(n.ClusterStmt.Indexname)
		}

	case *pg_query.Node_XmlSerialize:
		output.Builder.WriteString("XMLSERIALIZE(")
		switch n.XmlSerialize.Xmloption {
		case pg_query.XmlOptionType_XMLOPTION_DOCUMENT:
			output.Builder.WriteString("DOCUMENT ")
		case pg_query.XmlOptionType_XMLOPTION_CONTENT:
			output.Builder.WriteString("CONTENT ")
		}
		output.writeNode(n.XmlSerialize.Expr)
		output.Builder.WriteString(" AS ")
		output.writeTypeName(n.XmlSerialize.TypeName)
		if n.XmlSerialize.Indent {
			output.Builder.WriteString(" INDENT")
		}
		output.Builder.WriteString(")")

	case *pg_query.Node_XmlExpr:
		switch n.XmlExpr.Op {
		case pg_query.XmlExprOp_IS_XMLCONCAT:
			output.Builder.WriteString("XMLCONCAT(")
			output.writeCommaSeparatedList(n.XmlExpr.Args)
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLELEMENT:
			output.Builder.WriteString("XMLELEMENT(NAME ")
			output.Builder.WriteString(quoteIdentifier(n.XmlExpr.Name))
			if len(n.XmlExpr.NamedArgs) > 0 {
				output.Builder.WriteString(", XMLATTRIBUTES(")
				output.writeCommaSeparatedList(n.XmlExpr.NamedArgs)
				output.Builder.WriteString(")")
			}
			if len(n.XmlExpr.Args) > 0 {
				output.Builder.WriteString(", ")
				output.writeCommaSeparatedList(n.XmlExpr.Args)
			}
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLFOREST:
			output.Builder.WriteString("XMLFOREST(")
			output.writeCommaSeparatedList(n.XmlExpr.NamedArgs)
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLPARSE:
			output.Builder.WriteString("XMLPARSE(")
			if n.XmlExpr.Xmloption == pg_query.XmlOptionType_XMLOPTION_DOCUMENT {
				output.Builder.WriteString("DOCUMENT ")
			} else {
				output.Builder.WriteString("CONTENT ")
			}
			// The second arg is the internal preserve_whitespace flag, not
			// part of the XMLPARSE(...) argument syntax.
			if len(n.XmlExpr.Args) > 0 {
				output.writeNode(n.XmlExpr.Args[0])
			}
			if len(n.XmlExpr.Args) > 1 {
				if b := n.XmlExpr.Args[1].GetAConst(); b != nil && b.GetBoolval().GetBoolval() {
					output.Builder.WriteString(" PRESERVE WHITESPACE")
				}
			}
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLPI:
			output.Builder.WriteString("XMLPI(NAME ")
			output.Builder.WriteString(quoteIdentifier(n.XmlExpr.Name))
			if len(n.XmlExpr.Args) > 0 {
				output.Builder.WriteString(", ")
				output.writeCommaSeparatedList(n.XmlExpr.Args)
			}
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLROOT:
			// Args: [xml, version, standalone-code]. NULL version means
			// VERSION NO VALUE; the standalone code maps to keywords.
			output.Builder.WriteString("XMLROOT(")
			if len(n.XmlExpr.Args) > 0 {
				output.writeNode(n.XmlExpr.Args[0])
			}
			if len(n.XmlExpr.Args) > 1 {
				output.Builder.WriteString(", VERSION ")
				if c := n.XmlExpr.Args[1].GetAConst(); c != nil && c.Isnull {
					output.Builder.WriteString("NO VALUE")
				} else {
					output.writeNode(n.XmlExpr.Args[1])
				}
			}
			if len(n.XmlExpr.Args) > 2 {
				switch n.XmlExpr.Args[2].GetAConst().GetIval().GetIval() {
				case 0:
					output.Builder.WriteString(", STANDALONE YES")
				case 1:
					output.Builder.WriteString(", STANDALONE NO")
				case 2:
					output.Builder.WriteString(", STANDALONE NO VALUE")
				}
			}
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLSERIALIZE:
			output.Builder.WriteString("XMLSERIALIZE(")
			if n.XmlExpr.Xmloption == pg_query.XmlOptionType_XMLOPTION_DOCUMENT {
				output.Builder.WriteString("DOCUMENT ")
			} else {
				output.Builder.WriteString("CONTENT ")
			}
			output.writeCommaSeparatedList(n.XmlExpr.Args)
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_DOCUMENT:
			if len(n.XmlExpr.Args) > 0 {
				output.writeNode(n.XmlExpr.Args[0])
			}
			output.Builder.WriteString(" IS DOCUMENT")
		default:
			warn("unsupported xml expr op: %s", n.XmlExpr.Op.String())
		}

	case *pg_query.Node_AArrayExpr:
		output.Builder.WriteString("ARRAY[")
		output.writeCommaSeparatedList(n.AArrayExpr.Elements)
		output.Builder.WriteString("]")

	case *pg_query.Node_AIndirection:
		// Simple references (columns, params, chained indirection) only need
		// parens when followed by a field access or .*, to distinguish from
		// schema-qualified names. Any other expression (function call, cast,
		// row, sublink, ...) always needs them: f(x).* and '{}'::jsonb['a']
		// do not parse.
		var needsParens bool
		switch n.AIndirection.Arg.GetNode().(type) {
		case *pg_query.Node_ColumnRef, *pg_query.Node_ParamRef, *pg_query.Node_AIndirection:
			if len(n.AIndirection.Indirection) > 0 &&
				(n.AIndirection.Indirection[0].GetString_() != nil || n.AIndirection.Indirection[0].GetAStar() != nil) {
				needsParens = true
			}
		default:
			needsParens = true
		}
		if needsParens {
			output.Builder.WriteString("(")
		}
		output.writeNode(n.AIndirection.Arg)
		if needsParens {
			output.Builder.WriteString(")")
		}
		output.writeOptIndirection(n.AIndirection.Indirection)

	case *pg_query.Node_RowExpr:
		if n.RowExpr.RowFormat == pg_query.CoercionForm_COERCE_EXPLICIT_CALL {
			output.Builder.WriteString("ROW(")
		} else {
			output.Builder.WriteString("(")
		}
		output.writeCommaSeparatedList(n.RowExpr.Args)
		output.Builder.WriteString(")")

	case *pg_query.Node_JsonObjectConstructor:
		output.writeJsonObjectConstructor(n.JsonObjectConstructor)

	case *pg_query.Node_JsonArrayConstructor:
		output.writeJsonArrayConstructor(n.JsonArrayConstructor)

	case *pg_query.Node_JsonArrayQueryConstructor:
		output.writeJsonArrayQueryConstructor(n.JsonArrayQueryConstructor)

	case *pg_query.Node_JsonObjectAgg:
		output.writeJsonObjectAgg(n.JsonObjectAgg)

	case *pg_query.Node_JsonArrayAgg:
		output.writeJsonArrayAgg(n.JsonArrayAgg)

	case *pg_query.Node_JsonKeyValue:
		output.writeJsonKeyValue(n.JsonKeyValue)

	case *pg_query.Node_JsonValueExpr:
		output.writeJsonValueExpr(n.JsonValueExpr)

	case *pg_query.Node_JsonParseExpr:
		output.writeJsonParseExpr(n.JsonParseExpr)

	case *pg_query.Node_JsonScalarExpr:
		output.writeJsonScalarExpr(n.JsonScalarExpr)

	case *pg_query.Node_JsonSerializeExpr:
		output.writeJsonSerializeExpr(n.JsonSerializeExpr)

	case *pg_query.Node_JsonIsPredicate:
		output.writeJsonIsPredicate(n.JsonIsPredicate)

	case *pg_query.Node_JsonFuncExpr:
		output.writeJsonFuncExpr(n.JsonFuncExpr)

	case *pg_query.Node_ConstraintsSetStmt:
		output.Builder.WriteString("SET CONSTRAINTS ")
		if len(n.ConstraintsSetStmt.Constraints) == 0 {
			output.Builder.WriteString("ALL")
		} else {
			output.writeCommaSeparatedList(n.ConstraintsSetStmt.Constraints)
		}
		if n.ConstraintsSetStmt.Deferred {
			output.Builder.WriteString(" DEFERRED")
		} else {
			output.Builder.WriteString(" IMMEDIATE")
		}

	case nil:
		// nothing

	default:
		// Unhandled expression inside a handled statement: deparse just this
		// expression. The statement-level fallbacks below would paste the
		// entire statement here, producing invalid SQL.
		if output.nodeDepth > 1 {
			if s, ok := deparseExprFallback(node); ok {
				warn("unsupported expression node %T, using deparse", n)
				output.Builder.WriteString(s)
				return
			}
			if s, ok := deparseRangeFallback(node); ok {
				warn("unsupported range node %T, using deparse", n)
				output.Builder.WriteString(s)
				return
			}
			// A statement nested in a utility wrapper (EXPLAIN, COPY (...)):
			// deparse just that statement rather than pasting the whole
			// enclosing statement into the middle of itself.
			if deparsed, err := pgDeparse(&pg_query.ParseResult{
				Stmts: []*pg_query.RawStmt{{Stmt: node}},
			}); err == nil {
				warn("unsupported statement node %T, using deparse", n)
				output.Builder.WriteString(strings.TrimRight(deparsed, ";"))
				return
			}
		}
		if output.tryStatementFallback() {
			return
		}
		warn("unexpected node: %T", n)
	}
}

// tryStatementFallback writes the entire current statement using pre-computed
// deparse text, native deparse, or the original SQL text, in that order.
// Returns false when no fallback source is available.
func (output *Printer) tryStatementFallback() bool {
	// Fallback 1: use pre-computed deparsed text (from augmented AST)
	if output.Deparsed != "" {
		output.Builder.WriteString(output.Deparsed)
		return true
	}
	// Fallback 2: deparse via pg_query (native)
	if output.RawStmt != nil {
		deparsed, err := pgDeparse(&pg_query.ParseResult{
			Stmts: []*pg_query.RawStmt{output.RawStmt},
		})
		if err == nil {
			output.Builder.WriteString(deparsed)
			return true
		}
	}
	// Fallback 3: extract original SQL text (WASM, where deparse is unavailable)
	if output.RawStmt != nil && output.OriginalSQL != "" {
		start := output.RawStmt.StmtLocation
		end := start + output.RawStmt.StmtLen
		if output.RawStmt.StmtLen == 0 {
			end = int32(len(output.OriginalSQL))
		}
		if start >= 0 && int(end) <= len(output.OriginalSQL) {
			raw := strings.TrimRight(output.OriginalSQL[start:end], "; \t\n")
			output.Builder.WriteString(raw)
			return true
		}
	}
	return false
}

// alterTableCmdSupported reports whether the AlterTableCmd writer handles
// the subcommand. Must stay in sync with the Node_AlterTableCmd switch.
func alterTableCmdSupported(c *pg_query.AlterTableCmd) bool {
	switch c.Subtype {
	case pg_query.AlterTableType_AT_AddColumn,
		pg_query.AlterTableType_AT_DropColumn,
		pg_query.AlterTableType_AT_AlterColumnType,
		pg_query.AlterTableType_AT_ColumnDefault,
		pg_query.AlterTableType_AT_SetNotNull,
		pg_query.AlterTableType_AT_DropNotNull,
		pg_query.AlterTableType_AT_AddConstraint,
		pg_query.AlterTableType_AT_DropConstraint,
		pg_query.AlterTableType_AT_AddIndex,
		pg_query.AlterTableType_AT_ChangeOwner,
		pg_query.AlterTableType_AT_AddIdentity,
		pg_query.AlterTableType_AT_EnableRowSecurity,
		pg_query.AlterTableType_AT_DisableRowSecurity,
		pg_query.AlterTableType_AT_ForceRowSecurity,
		pg_query.AlterTableType_AT_NoForceRowSecurity,
		pg_query.AlterTableType_AT_ValidateConstraint,
		pg_query.AlterTableType_AT_AttachPartition,
		pg_query.AlterTableType_AT_DetachPartition,
		pg_query.AlterTableType_AT_DetachPartitionFinalize,
		pg_query.AlterTableType_AT_SetStatistics,
		pg_query.AlterTableType_AT_SetStorage,
		pg_query.AlterTableType_AT_SetCompression,
		pg_query.AlterTableType_AT_DropIdentity,
		pg_query.AlterTableType_AT_DropExpression,
		pg_query.AlterTableType_AT_SetLogged,
		pg_query.AlterTableType_AT_SetUnLogged,
		pg_query.AlterTableType_AT_SetRelOptions,
		pg_query.AlterTableType_AT_ResetRelOptions,
		pg_query.AlterTableType_AT_SetOptions,
		pg_query.AlterTableType_AT_ResetOptions,
		pg_query.AlterTableType_AT_AddInherit,
		pg_query.AlterTableType_AT_DropInherit,
		pg_query.AlterTableType_AT_ClusterOn,
		pg_query.AlterTableType_AT_DropCluster,
		pg_query.AlterTableType_AT_SetTableSpace,
		pg_query.AlterTableType_AT_SetAccessMethod,
		pg_query.AlterTableType_AT_EnableTrig,
		pg_query.AlterTableType_AT_EnableAlwaysTrig,
		pg_query.AlterTableType_AT_EnableReplicaTrig,
		pg_query.AlterTableType_AT_DisableTrig,
		pg_query.AlterTableType_AT_EnableTrigAll,
		pg_query.AlterTableType_AT_DisableTrigAll,
		pg_query.AlterTableType_AT_EnableTrigUser,
		pg_query.AlterTableType_AT_DisableTrigUser,
		pg_query.AlterTableType_AT_EnableRule,
		pg_query.AlterTableType_AT_EnableAlwaysRule,
		pg_query.AlterTableType_AT_EnableReplicaRule,
		pg_query.AlterTableType_AT_DisableRule,
		pg_query.AlterTableType_AT_ReplicaIdentity:
		return true
	}
	return false
}

// writeIndexConstraintTail emits the clauses shared by PRIMARY KEY, UNIQUE,
// and EXCLUDE constraints: INCLUDE, USING INDEX, and deferrability.
func (output *Printer) writeIndexConstraintTail(c *pg_query.Constraint) {
	if len(c.Including) > 0 {
		output.Builder.WriteString(" INCLUDE (")
		output.writeQuotedIdentifierList(c.Including)
		output.Builder.WriteString(")")
	}
	if c.Indexname != "" {
		output.Builder.WriteString(" USING INDEX ")
		output.Builder.WriteString(c.Indexname)
	}
	if c.Deferrable {
		output.Builder.WriteString(" DEFERRABLE")
	}
	if c.Initdeferred {
		output.Builder.WriteString(" INITIALLY DEFERRED")
	}
}

// grantObjectKeyword returns the object-class prefix for GRANT/REVOKE ... ON,
// including the trailing space ("" for plain tables). The second return value
// is false for unsupported combinations.
func grantObjectKeyword(target pg_query.GrantTargetType, obj pg_query.ObjectType) (string, bool) {
	if target == pg_query.GrantTargetType_ACL_TARGET_ALL_IN_SCHEMA {
		switch obj {
		case pg_query.ObjectType_OBJECT_TABLE:
			return "ALL TABLES IN SCHEMA ", true
		case pg_query.ObjectType_OBJECT_SEQUENCE:
			return "ALL SEQUENCES IN SCHEMA ", true
		case pg_query.ObjectType_OBJECT_FUNCTION:
			return "ALL FUNCTIONS IN SCHEMA ", true
		case pg_query.ObjectType_OBJECT_PROCEDURE:
			return "ALL PROCEDURES IN SCHEMA ", true
		case pg_query.ObjectType_OBJECT_ROUTINE:
			return "ALL ROUTINES IN SCHEMA ", true
		}
		return "", false
	}
	switch obj {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_COLUMN:
		return "", true // TABLE keyword is optional; deparse omits it too
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return "SEQUENCE ", true
	case pg_query.ObjectType_OBJECT_DATABASE:
		return "DATABASE ", true
	case pg_query.ObjectType_OBJECT_DOMAIN:
		return "DOMAIN ", true
	case pg_query.ObjectType_OBJECT_FDW:
		return "FOREIGN DATA WRAPPER ", true
	case pg_query.ObjectType_OBJECT_FOREIGN_SERVER:
		return "FOREIGN SERVER ", true
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return "FUNCTION ", true
	case pg_query.ObjectType_OBJECT_PROCEDURE:
		return "PROCEDURE ", true
	case pg_query.ObjectType_OBJECT_ROUTINE:
		return "ROUTINE ", true
	case pg_query.ObjectType_OBJECT_LANGUAGE:
		return "LANGUAGE ", true
	case pg_query.ObjectType_OBJECT_LARGEOBJECT:
		return "LARGE OBJECT ", true
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return "SCHEMA ", true
	case pg_query.ObjectType_OBJECT_TABLESPACE:
		return "TABLESPACE ", true
	case pg_query.ObjectType_OBJECT_TYPE:
		return "TYPE ", true
	case pg_query.ObjectType_OBJECT_PARAMETER_ACL:
		return "PARAMETER ", true
	}
	return "", false
}

// objectTypeKeyword maps an ObjectType to its DDL keyword (as used in DROP
// and COMMENT ON). The second return value is false for object types the
// printer cannot reference yet.
func objectTypeKeyword(t pg_query.ObjectType) (string, bool) {
	switch t {
	case pg_query.ObjectType_OBJECT_TABLE:
		return "TABLE", true
	case pg_query.ObjectType_OBJECT_COLUMN:
		return "COLUMN", true
	case pg_query.ObjectType_OBJECT_INDEX:
		return "INDEX", true
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return "SEQUENCE", true
	case pg_query.ObjectType_OBJECT_VIEW:
		return "VIEW", true
	case pg_query.ObjectType_OBJECT_MATVIEW:
		return "MATERIALIZED VIEW", true
	case pg_query.ObjectType_OBJECT_FOREIGN_TABLE:
		return "FOREIGN TABLE", true
	case pg_query.ObjectType_OBJECT_TYPE:
		return "TYPE", true
	case pg_query.ObjectType_OBJECT_DOMAIN:
		return "DOMAIN", true
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return "SCHEMA", true
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return "FUNCTION", true
	case pg_query.ObjectType_OBJECT_PROCEDURE:
		return "PROCEDURE", true
	case pg_query.ObjectType_OBJECT_ROUTINE:
		return "ROUTINE", true
	case pg_query.ObjectType_OBJECT_AGGREGATE:
		return "AGGREGATE", true
	case pg_query.ObjectType_OBJECT_TRIGGER:
		return "TRIGGER", true
	case pg_query.ObjectType_OBJECT_RULE:
		return "RULE", true
	case pg_query.ObjectType_OBJECT_POLICY:
		return "POLICY", true
	case pg_query.ObjectType_OBJECT_EVENT_TRIGGER:
		return "EVENT TRIGGER", true
	case pg_query.ObjectType_OBJECT_EXTENSION:
		return "EXTENSION", true
	case pg_query.ObjectType_OBJECT_TABCONSTRAINT, pg_query.ObjectType_OBJECT_DOMCONSTRAINT:
		return "CONSTRAINT", true
	case pg_query.ObjectType_OBJECT_COLLATION:
		return "COLLATION", true
	case pg_query.ObjectType_OBJECT_CONVERSION:
		return "CONVERSION", true
	case pg_query.ObjectType_OBJECT_LANGUAGE:
		return "LANGUAGE", true
	case pg_query.ObjectType_OBJECT_LARGEOBJECT:
		return "LARGE OBJECT", true
	case pg_query.ObjectType_OBJECT_ROLE:
		return "ROLE", true
	case pg_query.ObjectType_OBJECT_DATABASE:
		return "DATABASE", true
	case pg_query.ObjectType_OBJECT_TABLESPACE:
		return "TABLESPACE", true
	case pg_query.ObjectType_OBJECT_STATISTIC_EXT:
		return "STATISTICS", true
	case pg_query.ObjectType_OBJECT_OPCLASS:
		return "OPERATOR CLASS", true
	case pg_query.ObjectType_OBJECT_OPFAMILY:
		return "OPERATOR FAMILY", true
	case pg_query.ObjectType_OBJECT_FDW:
		return "FOREIGN DATA WRAPPER", true
	case pg_query.ObjectType_OBJECT_FOREIGN_SERVER:
		return "SERVER", true
	case pg_query.ObjectType_OBJECT_PUBLICATION:
		return "PUBLICATION", true
	case pg_query.ObjectType_OBJECT_ACCESS_METHOD:
		return "ACCESS METHOD", true
	case pg_query.ObjectType_OBJECT_TSCONFIGURATION:
		return "TEXT SEARCH CONFIGURATION", true
	case pg_query.ObjectType_OBJECT_TSDICTIONARY:
		return "TEXT SEARCH DICTIONARY", true
	case pg_query.ObjectType_OBJECT_TSPARSER:
		return "TEXT SEARCH PARSER", true
	case pg_query.ObjectType_OBJECT_TSTEMPLATE:
		return "TEXT SEARCH TEMPLATE", true
	case pg_query.ObjectType_OBJECT_CAST:
		return "CAST", true
	}
	return "", false
}

// writeObjectRef emits the object reference of a DROP or COMMENT ON
// statement, handling the sub-object syntaxes (name ON table, USING method,
// CAST (a AS b)).
func (output *Printer) writeObjectRef(t pg_query.ObjectType, obj *pg_query.Node) {
	switch t {
	case pg_query.ObjectType_OBJECT_TRIGGER,
		pg_query.ObjectType_OBJECT_RULE,
		pg_query.ObjectType_OBJECT_POLICY,
		pg_query.ObjectType_OBJECT_TABCONSTRAINT:
		// [schema?, table, name] → "name ON schema.table"
		if l := obj.GetList(); l != nil && len(l.Items) >= 2 {
			items := l.Items
			output.Builder.WriteString(quoteIdentifier(items[len(items)-1].GetString_().GetSval()))
			output.Builder.WriteString(" ON ")
			output.writeQuotedQualifiedName(items[:len(items)-1])
			return
		}
	case pg_query.ObjectType_OBJECT_AGGREGATE:
		// agg() is not valid aggregate syntax; an empty specified arg list
		// means agg(*).
		if owa := obj.GetObjectWithArgs(); owa != nil && !owa.ArgsUnspecified && len(owa.Objargs) == 0 {
			output.writeQuotedQualifiedName(owa.Objname)
			output.Builder.WriteString("(*)")
			return
		}
	case pg_query.ObjectType_OBJECT_DOMCONSTRAINT:
		// [domain TypeName, name] → "name ON DOMAIN domain"
		if l := obj.GetList(); l != nil && len(l.Items) == 2 {
			output.Builder.WriteString(quoteIdentifier(l.Items[1].GetString_().GetSval()))
			output.Builder.WriteString(" ON DOMAIN ")
			output.writeNode(l.Items[0])
			return
		}
	case pg_query.ObjectType_OBJECT_OPCLASS,
		pg_query.ObjectType_OBJECT_OPFAMILY:
		// [method, name...] → "name USING method"
		if l := obj.GetList(); l != nil && len(l.Items) >= 2 {
			output.writeListWithSeparator(l.Items[1:], ".")
			output.Builder.WriteString(" USING ")
			output.writeNode(l.Items[0])
			return
		}
	case pg_query.ObjectType_OBJECT_CAST:
		// [source TypeName, target TypeName] → "(source AS target)"
		if l := obj.GetList(); l != nil && len(l.Items) == 2 {
			output.Builder.WriteString("(")
			output.writeNode(l.Items[0])
			output.Builder.WriteString(" AS ")
			output.writeNode(l.Items[1])
			output.Builder.WriteString(")")
			return
		}
	}
	if tn := obj.GetTypeName(); tn != nil {
		output.writeQuotedQualifiedName(tn.Names)
		return
	}
	if l := obj.GetList(); l != nil {
		output.writeQuotedQualifiedName(l.Items)
		return
	}
	if str := obj.GetString_(); str != nil {
		output.Builder.WriteString(quoteIdentifier(str.Sval))
		return
	}
	output.writeNode(obj)
}

// writeRoleSpec emits a role reference (owner, grantee, ...).
func (output *Printer) writeRoleSpec(r *pg_query.RoleSpec) {
	if r == nil {
		warn("missing role spec")
		return
	}
	switch r.Roletype {
	case pg_query.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		output.Builder.WriteString("CURRENT_ROLE")
	case pg_query.RoleSpecType_ROLESPEC_CURRENT_USER:
		output.Builder.WriteString("CURRENT_USER")
	case pg_query.RoleSpecType_ROLESPEC_SESSION_USER:
		output.Builder.WriteString("SESSION_USER")
	case pg_query.RoleSpecType_ROLESPEC_PUBLIC:
		output.Builder.WriteString("PUBLIC")
	default:
		output.Builder.WriteString(quoteIdentifier(r.Rolename))
	}
}

// writeAlterColumnRef emits the column reference of an ALTER COLUMN
// subcommand: a name, or a column number for ALTER INDEX.
func (output *Printer) writeAlterColumnRef(c *pg_query.AlterTableCmd) {
	if c.Name != "" {
		output.Builder.WriteString(quoteIdentifier(c.Name))
	} else {
		output.Builder.WriteString(strconv.Itoa(int(c.Num)))
	}
}

func (output *Printer) writeTypeName(stmt *pg_query.TypeName) {
	var skipTypmods bool
	if stmt.Setof {
		output.Builder.WriteString("SETOF ")
	}
	if len(stmt.Names) == 2 && stmt.Names[0].GetString_().GetSval() == "pg_catalog" {
		switch stmt.Names[1].GetString_().GetSval() {
		case "bpchar":
			// Typmod-less bpchar is its own type; bare "char" re-parses
			// with an implicit (1) typmod.
			if len(stmt.Typmods) == 0 {
				output.Builder.WriteString("bpchar")
			} else {
				output.Builder.WriteString("char")
			}
		case "char":
			// The internal single-byte type must stay quoted; bare char
			// means bpchar(1).
			output.Builder.WriteString(`"char"`)
		case "bool":
			output.Builder.WriteString("boolean")
		case "int2":
			output.Builder.WriteString("smallint")
		case "int4":
			output.Builder.WriteString("int")
		case "int8":
			output.Builder.WriteString("bigint")
		case "float4":
			output.Builder.WriteString("real")
		case "float8":
			output.Builder.WriteString("double precision")
		case "varchar", "numeric", "real", "time", "timestamp":
			output.Builder.WriteString(stmt.Names[1].GetString_().GetSval())
		case "timetz", "timestamptz":
			output.Builder.WriteString(stmt.Names[1].GetString_().GetSval())
			if len(stmt.Typmods) > 0 {
				output.Builder.WriteString("(")
				output.writeCommaSeparatedList(stmt.Typmods)
				output.Builder.WriteString(")")
			}
			skipTypmods = true
		case "interval":
			output.Builder.WriteString("interval")
			if len(stmt.Typmods) > 0 {
				skipTypmods = true
				fields := stmt.Typmods[0].GetAConst().GetIval().GetIval()
				switch fields {
				case 1 << YEAR:
					output.Builder.WriteString(" year")
				case 1 << MONTH:
					output.Builder.WriteString(" month")
				case 1 << DAY:
					output.Builder.WriteString(" day")
				case 1 << HOUR:
					output.Builder.WriteString(" hour")
				case 1 << MINUTE:
					output.Builder.WriteString(" minute")
				case 1 << SECOND:
					output.Builder.WriteString(" second")
				case 1<<YEAR | 1<<MONTH:
					output.Builder.WriteString(" year to month")
				case 1<<DAY | 1<<HOUR:
					output.Builder.WriteString(" day to hour")
				case 1<<DAY | 1<<HOUR | 1<<MINUTE:
					output.Builder.WriteString(" day to minute")
				case 1<<DAY | 1<<HOUR | 1<<MINUTE | 1<<SECOND:
					output.Builder.WriteString(" day to second")
				case 1<<HOUR | 1<<MINUTE:
					output.Builder.WriteString(" hour to minute")
				case 1<<HOUR | 1<<MINUTE | 1<<SECOND:
					output.Builder.WriteString(" hour to second")
				case 1<<MINUTE | 1<<SECOND:
					output.Builder.WriteString(" minute to second")
				case 0x7FFF: // INTERVAL_FULL_RANGE
				default:
					warn("invalid interval fields: %d", fields)
				}

				if len(stmt.Typmods) == 2 {
					precision := stmt.Typmods[1].GetAConst().GetIval().GetIval()
					if precision != 0xFFFF { // INTERVAL_FULL_PRECISION
						output.Builder.WriteString(fmt.Sprintf("(%d)", precision))
					}
				}
			}
		default:
			output.Builder.WriteString("pg_catalog.")
			output.Builder.WriteString(stmt.Names[1].GetString_().GetSval())

		}
	} else {
		for i, n := range stmt.Names {
			sval := n.GetString_().GetSval()
			if sval == "char" {
				// The internal single-byte type must stay quoted; bare
				// char means bpchar(1).
				output.Builder.WriteString(`"char"`)
			} else {
				output.Builder.WriteString(quoteIdentifier(sval))
			}
			if i != len(stmt.Names)-1 {
				output.Builder.WriteString(".")
			}
		}
	}

	if !skipTypmods && len(stmt.Typmods) > 0 {
		output.Builder.WriteString("(")
		output.writeCommaSeparatedList(stmt.Typmods)
		output.Builder.WriteString(")")
	}

	for _, a := range stmt.ArrayBounds {
		output.Builder.WriteString("[")
		if i := a.GetInteger(); i != nil && i.GetIval() != -1 {
			output.writeNode(a)
		}
		output.Builder.WriteString("]")
	}

	if stmt.PctType {
		output.Builder.WriteString("%type")
	}
}

// Field types for time decoding.
//
// Can't have more of these than there are bits in an unsigned int
// since these are turned into bit masks during parsing and decoding.
//
// Furthermore, the values for YEAR, MONTH, DAY, HOUR, MINUTE, SECOND
// must be in the range 0..14 so that the associated bitmasks can fit
// into the left half of an INTERVAL's typmod value.  Since those bits
// are stored in typmods, you can't change them without initdb!
const (
	RESERV = iota
	MONTH
	YEAR
	DAY
	JULIAN
	TZ    /* fixed-offset timezone abbreviation */
	DTZ   /* fixed-offset timezone abbrev, DST */
	DYNTZ /* dynamic timezone abbreviation */
	IGNORE_DTF
	AMPM
	HOUR
	MINUTE
	SECOND
	MILLISECOND
	MICROSECOND
)
