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
			// No newline found; append at the end
			output.Builder.WriteString(toInsert.String())
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
	case *pg_query.Node_Integer:
		output.Builder.WriteString(strconv.Itoa(int(n.Integer.Ival)))
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
			output.Builder.WriteString(v.Bsval.Bsval)
		case nil:
			if n.AConst.Isnull {
				output.Builder.WriteString("NULL")
			}
		}
	case *pg_query.Node_AExpr:

		switch n.AExpr.Kind {
		case pg_query.A_Expr_Kind_AEXPR_OP:
			if n.AExpr.Lexpr != nil {
				output.writeExprWithParensIfNeeded(n.AExpr.Lexpr)
				output.Builder.WriteString(" ")
			}
			output.writeQualOp(n.AExpr.Name)
			if n.AExpr.Rexpr != nil {
				output.Builder.WriteString(" ")
				output.writeExprWithParensIfNeeded(n.AExpr.Rexpr)
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
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" IS DISTINCT FROM ")
			output.writeNode(n.AExpr.Rexpr)
		case pg_query.A_Expr_Kind_AEXPR_NOT_DISTINCT:
			output.writeNode(n.AExpr.Lexpr)
			output.Builder.WriteString(" IS NOT DISTINCT FROM ")
			output.writeNode(n.AExpr.Rexpr)
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
			output.writeNode(n.AExpr.Rexpr)
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
		output.writeListWithSeparator(n.FuncCall.Funcname, ".")
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
			output.writeNode(f)
		}

	case *pg_query.Node_CommonTableExpr:
		output.Builder.WriteString(n.CommonTableExpr.Ctename) // TODO deparseColId

		if len(n.CommonTableExpr.Aliascolnames) > 0 {
			output.Builder.WriteString("(")
			for _, f := range n.CommonTableExpr.Aliascolnames {
				output.writeNode(f)
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
			output.Builder.WriteString(n.IndexElem.Name)
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
			for _, o := range n.IndexElem.Collation {
				output.writeNode(o)
			}
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
			output.Builder.WriteString(" WITH ")
			for _, o := range n.IndexStmt.Options {
				output.writeNode(o)
			}
		}

		if n.IndexStmt.TableSpace != "" {
			output.Builder.WriteString(" TABLESPACE ")
			output.Builder.WriteString(n.IndexStmt.TableSpace)
		}

		if n.IndexStmt.WhereClause != nil {
			output.Builder.WriteString(" WHERE ")
			output.writeNode(n.IndexStmt.WhereClause)
		}

	case *pg_query.Node_RangeVar:
		output.writeRangeVar(n.RangeVar)

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
		// Functions is a list of lists; each inner list is [funcexpr, coldeflist]
		for i, fn := range n.RangeFunction.Functions {
			if l := fn.GetList(); l != nil && len(l.Items) > 0 {
				output.writeNode(l.Items[0])
			} else {
				output.writeNode(fn)
			}
			if i != len(n.RangeFunction.Functions)-1 {
				output.Builder.WriteString(", ")
			}
		}
		if n.RangeFunction.Ordinality {
			output.Builder.WriteString(" WITH ORDINALITY")
		}
		if n.RangeFunction.Alias != nil {
			output.Builder.WriteString(" ")
			output.writeAlias(n.RangeFunction.Alias)
		}

	case *pg_query.Node_JoinExpr:
		output.writeNode(n.JoinExpr.Larg)
		switch n.JoinExpr.Jointype {
		case pg_query.JoinType_JOIN_INNER:
			if n.JoinExpr.IsNatural {
				output.Builder.WriteString(" NATURAL JOIN ")
			} else if n.JoinExpr.Quals != nil {
				output.writeNewlineIndent()
				output.Builder.WriteString("\tJOIN ")
			} else {
				output.writeNewlineIndent()
				output.Builder.WriteString("\tCROSS JOIN ")
			}
		case pg_query.JoinType_JOIN_LEFT:
			output.writeNewlineIndent()
			output.Builder.WriteString("\tLEFT JOIN ")
		case pg_query.JoinType_JOIN_FULL:
			output.writeNewlineIndent()
			output.Builder.WriteString("\tFULL JOIN ")
		case pg_query.JoinType_JOIN_RIGHT:
			output.writeNewlineIndent()
			output.Builder.WriteString("\tRIGHT JOIN ")
		default:
			output.writeNewlineIndent()
			output.Builder.WriteString("\tJOIN ")
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
		}
		if n.JoinExpr.Alias != nil {
			output.Builder.WriteString(" AS ")
			output.writeAlias(n.JoinExpr.Alias)
		}

	case *pg_query.Node_ResTarget:
		output.writeNode(n.ResTarget.Val)
		if n.ResTarget.Name != "" {
			output.Builder.WriteString(" AS ")
			output.Builder.WriteString(n.ResTarget.Name)
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
				output.Builder.WriteString(c.GetResTarget().Name)
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
			output.writeNode(n.TypeCast.Arg)
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
		for i, t := range n.UpdateStmt.TargetList {
			rt := t.GetResTarget()
			output.Builder.WriteString(rt.Name)
			output.writeOptIndirection(rt.Indirection)
			output.Builder.WriteString(" = ")
			output.writeNode(rt.Val)
			if i != len(n.UpdateStmt.TargetList)-1 {
				output.Builder.WriteString(",\n\t")
			}
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
		if len(n.CreateStmt.InhRelations) > 0 {
			output.Builder.WriteString(" INHERITS (")
			output.writeCommaSeparatedList(n.CreateStmt.InhRelations)
			output.Builder.WriteString(")")
		}

	case *pg_query.Node_ColumnDef:
		output.Builder.WriteString(quoteIdentifier(n.ColumnDef.Colname))
		output.Builder.WriteString(" ")
		output.writeTypeName(n.ColumnDef.TypeName)
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
			output.writeNode(n.Constraint.RawExpr)
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
				output.Builder.WriteString(n.Constraint.Conname)
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
		case pg_query.ConstrType_CONSTR_PRIMARY:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(n.Constraint.Conname)
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("PRIMARY KEY")
			if len(n.Constraint.Keys) > 0 {
				output.Builder.WriteString(" (")
				output.writeCommaSeparatedList(n.Constraint.Keys)
				output.Builder.WriteString(")")
			}
		case pg_query.ConstrType_CONSTR_UNIQUE:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(n.Constraint.Conname)
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("UNIQUE")
			if len(n.Constraint.Keys) > 0 {
				output.Builder.WriteString(" (")
				output.writeCommaSeparatedList(n.Constraint.Keys)
				output.Builder.WriteString(")")
			}
		case pg_query.ConstrType_CONSTR_FOREIGN:
			if n.Constraint.Conname != "" {
				output.Builder.WriteString("CONSTRAINT ")
				output.Builder.WriteString(n.Constraint.Conname)
				output.Builder.WriteString(" ")
			}
			output.Builder.WriteString("REFERENCES ")
			output.writeRangeVar(n.Constraint.Pktable)
			if len(n.Constraint.PkAttrs) > 0 {
				output.Builder.WriteString(" (")
				output.writeCommaSeparatedList(n.Constraint.PkAttrs)
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
		default:
			warn("unsupported constraint type: %s", n.Constraint.Contype.String())
		}

	case *pg_query.Node_AlterTableStmt:
		output.Builder.WriteString("ALTER TABLE ")
		if n.AlterTableStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
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
			output.Builder.WriteString(n.AlterTableCmd.Name)
		case pg_query.AlterTableType_AT_AlterColumnType:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(n.AlterTableCmd.Name)
			output.Builder.WriteString(" TYPE ")
			if def := n.AlterTableCmd.Def; def != nil {
				if cd := def.GetColumnDef(); cd != nil {
					output.writeTypeName(cd.TypeName)
				}
			}
		case pg_query.AlterTableType_AT_ColumnDefault:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(n.AlterTableCmd.Name)
			if n.AlterTableCmd.Def != nil {
				output.Builder.WriteString(" SET DEFAULT ")
				output.writeNode(n.AlterTableCmd.Def)
			} else {
				output.Builder.WriteString(" DROP DEFAULT")
			}
		case pg_query.AlterTableType_AT_SetNotNull:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(n.AlterTableCmd.Name)
			output.Builder.WriteString(" SET NOT NULL")
		case pg_query.AlterTableType_AT_DropNotNull:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(n.AlterTableCmd.Name)
			output.Builder.WriteString(" DROP NOT NULL")
		case pg_query.AlterTableType_AT_AddConstraint:
			output.Builder.WriteString("ADD ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_DropConstraint:
			output.Builder.WriteString("DROP CONSTRAINT ")
			if n.AlterTableCmd.MissingOk {
				output.Builder.WriteString("IF EXISTS ")
			}
			output.Builder.WriteString(n.AlterTableCmd.Name)
		case pg_query.AlterTableType_AT_AddIndex:
			output.Builder.WriteString("ADD INDEX ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_ChangeOwner:
			output.Builder.WriteString("OWNER TO ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_AddIdentity:
			output.Builder.WriteString("ALTER COLUMN ")
			output.Builder.WriteString(n.AlterTableCmd.Name)
			output.Builder.WriteString(" ADD ")
			output.writeNode(n.AlterTableCmd.Def)
		case pg_query.AlterTableType_AT_EnableRowSecurity:
			output.Builder.WriteString("ENABLE ROW LEVEL SECURITY")
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
		output.Builder.WriteString("DROP ")
		switch n.DropStmt.RemoveType {
		case pg_query.ObjectType_OBJECT_TABLE:
			output.Builder.WriteString("TABLE ")
		case pg_query.ObjectType_OBJECT_INDEX:
			output.Builder.WriteString("INDEX ")
		case pg_query.ObjectType_OBJECT_SEQUENCE:
			output.Builder.WriteString("SEQUENCE ")
		case pg_query.ObjectType_OBJECT_VIEW:
			output.Builder.WriteString("VIEW ")
		case pg_query.ObjectType_OBJECT_MATVIEW:
			output.Builder.WriteString("MATERIALIZED VIEW ")
		case pg_query.ObjectType_OBJECT_TYPE:
			output.Builder.WriteString("TYPE ")
		case pg_query.ObjectType_OBJECT_SCHEMA:
			output.Builder.WriteString("SCHEMA ")
		case pg_query.ObjectType_OBJECT_FUNCTION:
			output.Builder.WriteString("FUNCTION ")
		case pg_query.ObjectType_OBJECT_PROCEDURE:
			output.Builder.WriteString("PROCEDURE ")
		case pg_query.ObjectType_OBJECT_TRIGGER:
			output.Builder.WriteString("TRIGGER ")
		case pg_query.ObjectType_OBJECT_EXTENSION:
			output.Builder.WriteString("EXTENSION ")
		case pg_query.ObjectType_OBJECT_DOMAIN:
			output.Builder.WriteString("DOMAIN ")
		default:
			warn("unsupported drop type: %s", n.DropStmt.RemoveType.String())
		}
		if n.DropStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
		}
		isTrigger := n.DropStmt.RemoveType == pg_query.ObjectType_OBJECT_TRIGGER
		for i, obj := range n.DropStmt.Objects {
			if tn := obj.GetTypeName(); tn != nil {
				output.writeListWithSeparator(tn.Names, ".")
			} else if l := obj.GetList(); l != nil {
				if isTrigger && len(l.Items) == 2 {
					// DROP TRIGGER: list is [table, trigger] → "trigger ON table"
					output.writeNode(l.Items[1])
					output.Builder.WriteString(" ON ")
					output.writeNode(l.Items[0])
				} else {
					output.writeListWithSeparator(l.Items, ".")
				}
			} else {
				output.writeNode(obj)
			}
			if i != len(n.DropStmt.Objects)-1 {
				output.Builder.WriteString(", ")
			}
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
		case pg_query.TransactionStmtKind_TRANS_STMT_COMMIT:
			output.Builder.WriteString("COMMIT")
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK:
			output.Builder.WriteString("ROLLBACK")
		case pg_query.TransactionStmtKind_TRANS_STMT_SAVEPOINT:
			output.Builder.WriteString("SAVEPOINT ")
			output.Builder.WriteString(n.TransactionStmt.SavepointName)
		case pg_query.TransactionStmtKind_TRANS_STMT_RELEASE:
			output.Builder.WriteString("RELEASE SAVEPOINT ")
			output.Builder.WriteString(n.TransactionStmt.SavepointName)
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO:
			output.Builder.WriteString("ROLLBACK TO SAVEPOINT ")
			output.Builder.WriteString(n.TransactionStmt.SavepointName)
		default:
			warn("unsupported transaction kind: %s", n.TransactionStmt.Kind.String())
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
				output.Builder.WriteString(" $$")
				output.formatPLpgSQLBody(asBody, 0)
				output.Builder.WriteString("\n$$")
			} else if strings.EqualFold(lang, "sql") {
				output.Builder.WriteString(" $$")
				output.formatSQLBody(asBody, 1)
				output.Builder.WriteString("\n$$")
			} else {
				output.Builder.WriteString(" $$\n")
				output.Builder.WriteString(asBody)
				output.Builder.WriteString("\n$$")
			}
		}

	case *pg_query.Node_CreateFunctionStmt:
		output.Builder.WriteString("CREATE ")
		if n.CreateFunctionStmt.Replace {
			output.Builder.WriteString("OR REPLACE ")
		}
		if n.CreateFunctionStmt.IsProcedure {
			output.Builder.WriteString("PROCEDURE ")
		} else {
			output.Builder.WriteString("FUNCTION ")
		}
		output.writeListWithSeparator(n.CreateFunctionStmt.Funcname, ".")
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
				output.Builder.WriteString("\nAS $$")
				output.formatSQLBody(asBody, 1)
				output.Builder.WriteString("\n$$")
			} else if strings.EqualFold(lang, "plpgsql") {
				output.Builder.WriteString("\nAS $$")
				output.formatPLpgSQLBody(asBody, 0)
				output.Builder.WriteString("\n$$")
			} else {
				output.Builder.WriteString("\nAS $$\n")
				output.Builder.WriteString(asBody)
				output.Builder.WriteString("\n$$")
			}
		}
		if n.CreateFunctionStmt.SqlBody != nil {
			output.Builder.WriteString("\n")
			output.writeNode(n.CreateFunctionStmt.SqlBody)
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
		output.Builder.WriteString(n.CreateSchemaStmt.Schemaname)

	case *pg_query.Node_CreateSeqStmt:
		output.Builder.WriteString("CREATE SEQUENCE ")
		output.writeRangeVar(n.CreateSeqStmt.Sequence)
		for _, opt := range n.CreateSeqStmt.Options {
			de := opt.GetDefElem()
			if de == nil {
				continue
			}
			switch de.Defname {
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
		if n.GrantStmt.IsGrant {
			output.Builder.WriteString("GRANT ")
		} else {
			output.Builder.WriteString("REVOKE ")
		}
		if len(n.GrantStmt.Privileges) == 0 {
			output.Builder.WriteString("ALL")
		} else {
			for i, p := range n.GrantStmt.Privileges {
				ap := p.GetAccessPriv()
				if ap != nil {
					output.Builder.WriteString(strings.ToUpper(ap.PrivName))
				}
				if i != len(n.GrantStmt.Privileges)-1 {
					output.Builder.WriteString(", ")
				}
			}
		}
		output.Builder.WriteString(" ON ")
		for i, obj := range n.GrantStmt.Objects {
			output.writeNode(obj)
			if i != len(n.GrantStmt.Objects)-1 {
				output.Builder.WriteString(", ")
			}
		}
		if n.GrantStmt.IsGrant {
			output.Builder.WriteString(" TO ")
		} else {
			output.Builder.WriteString(" FROM ")
		}
		for i, g := range n.GrantStmt.Grantees {
			rs := g.GetRoleSpec()
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
			if i != len(n.GrantStmt.Grantees)-1 {
				output.Builder.WriteString(", ")
			}
		}

	case *pg_query.Node_CommentStmt:
		output.Builder.WriteString("COMMENT ON ")
		switch n.CommentStmt.Objtype {
		case pg_query.ObjectType_OBJECT_TABLE:
			output.Builder.WriteString("TABLE ")
		case pg_query.ObjectType_OBJECT_COLUMN:
			output.Builder.WriteString("COLUMN ")
		case pg_query.ObjectType_OBJECT_INDEX:
			output.Builder.WriteString("INDEX ")
		case pg_query.ObjectType_OBJECT_FUNCTION:
			output.Builder.WriteString("FUNCTION ")
		case pg_query.ObjectType_OBJECT_SCHEMA:
			output.Builder.WriteString("SCHEMA ")
		case pg_query.ObjectType_OBJECT_SEQUENCE:
			output.Builder.WriteString("SEQUENCE ")
		case pg_query.ObjectType_OBJECT_VIEW:
			output.Builder.WriteString("VIEW ")
		case pg_query.ObjectType_OBJECT_TYPE:
			output.Builder.WriteString("TYPE ")
		case pg_query.ObjectType_OBJECT_DOMAIN:
			output.Builder.WriteString("DOMAIN ")
		case pg_query.ObjectType_OBJECT_TRIGGER:
			output.Builder.WriteString("TRIGGER ")
		case pg_query.ObjectType_OBJECT_EXTENSION:
			output.Builder.WriteString("EXTENSION ")
		case pg_query.ObjectType_OBJECT_TABCONSTRAINT:
			output.Builder.WriteString("CONSTRAINT ")
		default:
			warn("unsupported comment object type: %s", n.CommentStmt.Objtype.String())
		}
		if n.CommentStmt.Objtype == pg_query.ObjectType_OBJECT_TABCONSTRAINT {
			// COMMENT ON CONSTRAINT constraint_name ON table_name
			if l := n.CommentStmt.Object.GetList(); l != nil && len(l.Items) == 2 {
				output.writeNode(l.Items[1]) // constraint name
				output.Builder.WriteString(" ON ")
				output.writeNode(l.Items[0]) // table name
			}
		} else if l := n.CommentStmt.Object.GetList(); l != nil {
			output.writeListWithSeparator(l.Items, ".")
		} else {
			output.writeNode(n.CommentStmt.Object)
		}
		output.Builder.WriteString(" IS '")
		output.Builder.WriteString(strings.ReplaceAll(n.CommentStmt.Comment, "'", "''"))
		output.Builder.WriteString("'")

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
		}
		if len(n.CopyStmt.Attlist) > 0 {
			output.Builder.WriteString(" (")
			output.writeCommaSeparatedList(n.CopyStmt.Attlist)
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
						output.Builder.WriteString(s.GetSval())
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
		output.Builder.WriteString(n.UnlistenStmt.Conditionname)

	case *pg_query.Node_VariableSetStmt:
		switch n.VariableSetStmt.Kind {
		case pg_query.VariableSetKind_VAR_SET_VALUE:
			output.Builder.WriteString("SET ")
			output.Builder.WriteString(n.VariableSetStmt.Name)
			output.Builder.WriteString(" TO ")
			output.writeCommaSeparatedList(n.VariableSetStmt.Args)
		case pg_query.VariableSetKind_VAR_SET_DEFAULT:
			output.Builder.WriteString("SET ")
			output.Builder.WriteString(n.VariableSetStmt.Name)
			output.Builder.WriteString(" TO DEFAULT")
		case pg_query.VariableSetKind_VAR_RESET:
			output.Builder.WriteString("RESET ")
			output.Builder.WriteString(n.VariableSetStmt.Name)
		case pg_query.VariableSetKind_VAR_RESET_ALL:
			output.Builder.WriteString("RESET ALL")
		default:
			warn("unsupported variable set kind: %s", n.VariableSetStmt.Kind.String())
		}

	case *pg_query.Node_VariableShowStmt:
		output.Builder.WriteString("SHOW ")
		output.Builder.WriteString(n.VariableShowStmt.Name)

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
		output.Builder.WriteString(n.DeallocateStmt.Name)

	case *pg_query.Node_VacuumStmt:
		if n.VacuumStmt.IsVacuumcmd {
			output.Builder.WriteString("VACUUM")
		} else {
			output.Builder.WriteString("ANALYZE")
		}
		// Check for ANALYZE option on VACUUM
		if n.VacuumStmt.IsVacuumcmd {
			hasOpts := false
			for _, opt := range n.VacuumStmt.Options {
				de := opt.GetDefElem()
				if de != nil && (de.Defname == "analyze" || de.Defname == "verbose") {
					if !hasOpts {
						output.Builder.WriteString(" (")
						hasOpts = true
					} else {
						output.Builder.WriteString(", ")
					}
					output.Builder.WriteString(strings.ToUpper(de.Defname))
				}
			}
			if hasOpts {
				output.Builder.WriteString(")")
			}
		}
		for _, rel := range n.VacuumStmt.Rels {
			vr := rel.GetVacuumRelation()
			if vr != nil && vr.Relation != nil {
				output.Builder.WriteString(" ")
				output.writeRangeVar(vr.Relation)
			}
		}

	case *pg_query.Node_VacuumRelation:
		if n.VacuumRelation.Relation != nil {
			output.writeRangeVar(n.VacuumRelation.Relation)
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
		output.writeListWithSeparator(n.CreateTrigStmt.Funcname, ".")
		output.Builder.WriteString("(")
		output.writeCommaSeparatedList(n.CreateTrigStmt.Args)
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

	case *pg_query.Node_XmlExpr:
		switch n.XmlExpr.Op {
		case pg_query.XmlExprOp_IS_XMLCONCAT:
			output.Builder.WriteString("XMLCONCAT(")
			output.writeCommaSeparatedList(n.XmlExpr.Args)
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLELEMENT:
			output.Builder.WriteString("XMLELEMENT(NAME ")
			output.Builder.WriteString(n.XmlExpr.Name)
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
			output.writeCommaSeparatedList(n.XmlExpr.Args)
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLPI:
			output.Builder.WriteString("XMLPI(NAME ")
			output.Builder.WriteString(n.XmlExpr.Name)
			if len(n.XmlExpr.Args) > 0 {
				output.Builder.WriteString(", ")
				output.writeCommaSeparatedList(n.XmlExpr.Args)
			}
			output.Builder.WriteString(")")
		case pg_query.XmlExprOp_IS_XMLROOT:
			output.Builder.WriteString("XMLROOT(")
			output.writeCommaSeparatedList(n.XmlExpr.Args)
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
		// Parentheses are needed around the arg when the first indirection
		// element is a field access (String_), to distinguish from
		// schema-qualified column references. Array subscripts don't need them.
		needsParens := false
		if len(n.AIndirection.Indirection) > 0 && n.AIndirection.Indirection[0].GetString_() != nil {
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
		}
		// Fallback 1: use pre-computed deparsed text (from augmented AST)
		if output.Deparsed != "" {
			output.Builder.WriteString(output.Deparsed)
			return
		}
		// Fallback 2: deparse via pg_query (native)
		if output.RawStmt != nil {
			deparsed, err := pgDeparse(&pg_query.ParseResult{
				Stmts: []*pg_query.RawStmt{output.RawStmt},
			})
			if err == nil {
				output.Builder.WriteString(deparsed)
				return
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
				warn("unsupported node %T, using original SQL", n)
				output.Builder.WriteString(raw)
				return
			}
		}
		warn("unexpected node: %T", n)
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
			output.Builder.WriteString("char")
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
			if len(stmt.Typmods) == 0 {
				output.Builder.WriteString("interval")
			} else {
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
					precision := stmt.Typmods[0].GetAConst().GetIval().GetIval()
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
			output.Builder.WriteString(quoteIdentifier(n.GetString_().GetSval()))
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
