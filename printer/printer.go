package printer

// https://github.com/pganalyze/libpg_query/blob/13-latest/src/pg_query_deparse.c

import (
	"fmt"
	"strconv"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
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

type Printer struct {
	Builder        *strings.Builder
	indent         int       // current indentation level
	comments       []comment // inline comments for the current statement
	commentIdx     int       // next inline comment to process
	lastNodeEndPos int       // output position after last node with a source location
	RawStmt        *pg_query.RawStmt // set by Format to enable deparse fallback
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
	defer func() {
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
			output.indent += 2
			output.writeNewlineIndent()
			output.writeNode(n.SubLink.Subselect)
			output.indent -= 2
			output.writeNewlineIndent()
			output.Builder.WriteString("\t)")
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
		for i, a := range n.FuncCall.Args {
			if n.FuncCall.FuncVariadic && i == len(n.FuncCall.Args)-1 {
				output.Builder.WriteString("VARIADIC ")
			}
			output.writeNode(a)

			if i != len(n.FuncCall.Args)-1 {
				output.Builder.WriteString(", ")
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
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeCommaSeparatedList(n.InsertStmt.ReturningList)
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
			output.writeCommaSeparatedList(n.UpdateStmt.ReturningList)
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
			output.writeCommaSeparatedList(n.DeleteStmt.ReturningList)
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
		output.Builder.WriteString(" (\n")
		for i, elt := range n.CreateStmt.TableElts {
			output.Builder.WriteString("\t")
			output.writeNode(elt)
			if i != len(n.CreateStmt.TableElts)-1 {
				output.Builder.WriteString(",")
			}
			output.Builder.WriteString("\n")
		}
		output.Builder.WriteString(")")
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
			output.Builder.WriteString("CHECK (")
			output.writeNode(n.Constraint.RawExpr)
			output.Builder.WriteString(")")
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
		default:
			warn("unsupported alter table cmd: %s", n.AlterTableCmd.Subtype.String())
		}

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
			output.Builder.WriteString("CURRENT_TIME")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP:
			output.Builder.WriteString("CURRENT_TIMESTAMP")
		case pg_query.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP_N:
			output.Builder.WriteString("CURRENT_TIMESTAMP")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIME:
			output.Builder.WriteString("LOCALTIME")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIME_N:
			output.Builder.WriteString("LOCALTIME")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP:
			output.Builder.WriteString("LOCALTIMESTAMP")
		case pg_query.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N:
			output.Builder.WriteString("LOCALTIMESTAMP")
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
				output.Builder.WriteString(rs.Rolename)
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
		default:
			warn("unsupported comment object type: %s", n.CommentStmt.Objtype.String())
		}
		if l := n.CommentStmt.Object.GetList(); l != nil {
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
			for _, opt := range n.ExplainStmt.Options {
				de := opt.GetDefElem()
				if de != nil && de.Defname == "analyze" {
					output.Builder.WriteString(" ANALYZE")
				} else if de != nil && de.Defname == "verbose" {
					output.Builder.WriteString(" VERBOSE")
				}
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
			output.Builder.WriteString(n.CopyStmt.Filename)
			output.Builder.WriteString("'")
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

	case nil:
		// nothing

	default:
		// Fallback: deparse the entire statement if possible
		if output.RawStmt != nil {
			deparsed, err := pg_query.Deparse(&pg_query.ParseResult{
				Stmts: []*pg_query.RawStmt{output.RawStmt},
			})
			if err == nil {
				output.Builder.WriteString(deparsed)
				return
			}
		}
		warn("unexpected node: %T", n)
	}
}

func (output *Printer) writeExprWithParensIfNeeded(node *pg_query.Node) {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_BoolExpr:
		// Multi-arg boolean expressions get block-style parens
		if len(n.BoolExpr.Args) > 1 {
			output.Builder.WriteString("(")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(node)
			output.indent--
			output.writeNewlineIndent()
			output.Builder.WriteString(")")
		} else {
			output.Builder.WriteString("(")
			output.writeNode(node)
			output.Builder.WriteString(")")
		}
	case *pg_query.Node_AExpr:
		output.Builder.WriteString("(")
		output.writeNode(node)
		output.Builder.WriteString(")")
	default:
		output.writeNode(node)
	}
}

func (output *Printer) writeOnConflictClause(clause *pg_query.OnConflictClause) {
	output.Builder.WriteString("ON CONFLICT ")
	if clause.Infer != nil {
		if len(clause.Infer.IndexElems) > 0 {
			output.Builder.WriteString("(")
			output.writeCommaSeparatedList(clause.Infer.IndexElems)
			output.Builder.WriteString(") ")
		}
		if clause.Infer.Conname != "" {
			output.Builder.WriteString("ON CONSTRAINT ")
			output.Builder.WriteString(clause.Infer.Conname)
			output.Builder.WriteString(" ")
		}
		if clause.Infer.WhereClause != nil {
			output.Builder.WriteString("WHERE ")
			output.writeNode(clause.Infer.WhereClause)
			output.Builder.WriteString(" ")
		}
	}
	switch clause.Action {
	case pg_query.OnConflictAction_ONCONFLICT_NOTHING:
		output.Builder.WriteString("DO NOTHING")
	case pg_query.OnConflictAction_ONCONFLICT_UPDATE:
		output.Builder.WriteString("DO UPDATE SET ")
		output.writeCommaSeparatedList(clause.TargetList)
		if clause.WhereClause != nil {
			output.Builder.WriteString(" WHERE ")
			output.writeNode(clause.WhereClause)
		}
	}
}

func (output *Printer) writeFkAction(prefix string, action string, setCols []*pg_query.Node) {
	switch action {
	case "a", "": // NO ACTION is default, omit
	case "r":
		output.Builder.WriteString(" ")
		output.Builder.WriteString(prefix)
		output.Builder.WriteString(" RESTRICT")
	case "c":
		output.Builder.WriteString(" ")
		output.Builder.WriteString(prefix)
		output.Builder.WriteString(" CASCADE")
	case "n":
		output.Builder.WriteString(" ")
		output.Builder.WriteString(prefix)
		output.Builder.WriteString(" SET NULL")
		if len(setCols) > 0 {
			output.Builder.WriteString(" (")
			output.writeCommaSeparatedList(setCols)
			output.Builder.WriteString(")")
		}
	case "d":
		output.Builder.WriteString(" ")
		output.Builder.WriteString(prefix)
		output.Builder.WriteString(" SET DEFAULT")
		if len(setCols) > 0 {
			output.Builder.WriteString(" (")
			output.writeCommaSeparatedList(setCols)
			output.Builder.WriteString(")")
		}
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

func (output *Printer) writeRangeVar(stmt *pg_query.RangeVar) {
	if stmt.Catalogname != "" {
		output.Builder.WriteString(stmt.Catalogname)
		output.Builder.WriteString(".")
	}
	if stmt.Schemaname != "" {
		output.Builder.WriteString(stmt.Schemaname)
		output.Builder.WriteString(".")
	}
	output.Builder.WriteString(stmt.Relname)
	if stmt.Alias != nil {
		output.Builder.WriteString(" AS ")
		output.writeAlias(stmt.Alias)
	}
}

func (output *Printer) writeSelectStmt(stmt *pg_query.SelectStmt) {
	if stmt.WithClause != nil {
		output.writeWithClause(stmt.WithClause)
	}

	switch stmt.Op {
	case pg_query.SetOperation_SETOP_NONE:
		if stmt.ValuesLists != nil {
			output.Builder.WriteString("VALUES ")
			for i, v := range stmt.ValuesLists {
				output.Builder.WriteString("(")
				if l := v.GetList(); l != nil {
					output.writeCommaSeparatedList(l.Items)
				} else {
					output.writeNode(v)
				}
				output.Builder.WriteString(")")
				if i != len(stmt.ValuesLists)-1 {
					output.Builder.WriteString(", ")
				}
			}
			return
		}

		output.Builder.WriteString("SELECT")
		if stmt.DistinctClause != nil {
			// Plain DISTINCT has a single nil node; DISTINCT ON has real column refs
			hasDistinctOn := false
			for _, d := range stmt.DistinctClause {
				if d.GetNode() != nil {
					hasDistinctOn = true
					break
				}
			}
			output.Builder.WriteString(" DISTINCT")
			if hasDistinctOn {
				output.Builder.WriteString(" ON (")
				output.writeCommaSeparatedList(stmt.DistinctClause)
				output.Builder.WriteString(")")
			}
		}
		output.writeNewlineIndent()
		output.Builder.WriteString("\t")
		for i, t := range stmt.TargetList {
			output.writeNode(t)
			if i != len(stmt.TargetList)-1 {
				output.Builder.WriteString(",")
				output.writeNewlineIndent()
				output.Builder.WriteString("\t")
			}
		}

		if stmt.IntoClause != nil {
			output.writeNewlineIndent()
			output.Builder.WriteString("INTO ")
			if stmt.IntoClause.Rel != nil {
				output.writeRangeVar(stmt.IntoClause.Rel)
			}
		}

		if len(stmt.FromClause) > 0 {
			output.writeNewlineIndent()
			output.Builder.WriteString("FROM")
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeListWithSeparator(stmt.FromClause, ", ")
		}
		if stmt.WhereClause != nil {
			output.writeNewlineIndent()
			output.Builder.WriteString("WHERE")
			output.indent++
			output.writeNewlineIndent()
			output.writeNode(stmt.WhereClause)
			output.indent--
		}

		if len(stmt.GroupClause) > 0 {
			output.writeNewlineIndent()
			output.Builder.WriteString("GROUP BY")
			if stmt.GroupDistinct {
				output.Builder.WriteString(" DISTINCT")
			}
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeCommaSeparatedList(stmt.GroupClause)
		}

		if stmt.HavingClause != nil {
			output.writeNewlineIndent()
			output.Builder.WriteString("HAVING")
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeNode(stmt.HavingClause)
		}

		if len(stmt.WindowClause) > 0 {
			output.writeNewlineIndent()
			output.Builder.WriteString("WINDOW")
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeCommaSeparatedList(stmt.WindowClause)
		}
	case pg_query.SetOperation_SETOP_UNION, pg_query.SetOperation_SETOP_INTERSECT, pg_query.SetOperation_SETOP_EXCEPT:
		output.Builder.WriteString("(")
		output.writeSelectStmt(stmt.Larg)
		output.Builder.WriteString(")")

		output.writeNewlineIndent()
		switch stmt.Op {
		case pg_query.SetOperation_SETOP_UNION:
			output.Builder.WriteString("UNION")
		case pg_query.SetOperation_SETOP_INTERSECT:
			output.Builder.WriteString("INTERSECT")
		case pg_query.SetOperation_SETOP_EXCEPT:
			output.Builder.WriteString("EXCEPT")
		default:
			warn("unexpected set operation")
		}

		if stmt.All {
			output.Builder.WriteString(" ALL")
		}

		output.writeNewlineIndent()
		output.Builder.WriteString("(")
		output.writeSelectStmt(stmt.Rarg)
		output.Builder.WriteString(")")
	}

	if len(stmt.SortClause) > 0 {
		output.writeNewlineIndent()
		output.Builder.WriteString("ORDER BY")
		output.writeNewlineIndent()
		output.Builder.WriteString("\t")
		output.writeCommaSeparatedList(stmt.SortClause)
	}

	if stmt.LimitCount != nil {
		output.writeNewlineIndent()
		switch stmt.LimitOption {
		case pg_query.LimitOption_LIMIT_OPTION_COUNT:
			output.Builder.WriteString("LIMIT ")
		case pg_query.LimitOption_LIMIT_OPTION_WITH_TIES:
			output.Builder.WriteString("FETCH FIRST ")
		}

		if stmt.LimitCount.GetAConst().GetIsnull() {
			output.Builder.WriteString("ALL")
		} else {
			output.writeNode(stmt.LimitCount)
		}

		if stmt.LimitOption == pg_query.LimitOption_LIMIT_OPTION_WITH_TIES {
			output.Builder.WriteString(" ROWS WITH TIES")
		}
	}

	if stmt.LimitOffset != nil {
		output.writeNewlineIndent()
		output.Builder.WriteString("OFFSET ")
		output.writeNode(stmt.LimitOffset)
	}

	if len(stmt.LockingClause) > 0 {
		output.writeNewlineIndent()
		output.writeCommaSeparatedList(stmt.LockingClause)
	}
}

func (output *Printer) writeQualOp(n []*pg_query.Node) {
	if len(n) == 1 {
		output.writeNode(n[0])
	} else {
		output.Builder.WriteString("OPERATOR (")
		for _, x := range n {
			output.writeNode(x)
		}
		output.Builder.WriteString(")")
	}
}

func (output *Printer) writeAlias(a *pg_query.Alias) {
	output.Builder.WriteString(a.Aliasname)
	if len(a.Colnames) > 0 {
		output.Builder.WriteString("(")
		for _, c := range a.Colnames {
			output.writeNode(c)
		}
		output.Builder.WriteString(")")
	}
}

func (output *Printer) formatSQLBody(body string, indentLevel int) {
	result, err := pgParse(body)
	if err != nil {
		// If we can't parse it, emit raw
		output.Builder.WriteString(body)
		return
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

func (output *Printer) writeFunctionParams(params []*pg_query.Node) {
	// Filter out TABLE params (they go in RETURNS TABLE, not the param list)
	var filtered []*pg_query.Node
	for _, p := range params {
		fp := p.GetFunctionParameter()
		if fp != nil && fp.Mode == pg_query.FunctionParameterMode_FUNC_PARAM_TABLE {
			continue
		}
		filtered = append(filtered, p)
	}

	// Render params to check total length
	b := &strings.Builder{}
	tmp := &Printer{Builder: b}
	tmp.writeCommaSeparatedList(filtered)
	inline := b.String()

	// Find how far we are on the current line
	s := output.Builder.String()
	lastNewline := strings.LastIndex(s, "\n")
	currentLineLen := len(s) - lastNewline - 1

	if currentLineLen+len(inline)+1 > 100 && len(filtered) > 1 {
		// Multi-line params
		output.indent++
		for i, p := range filtered {
			output.writeNewlineIndent()
			output.writeNode(p)
			if i != len(filtered)-1 {
				output.Builder.WriteString(",")
			}
		}
		output.indent--
		output.writeNewlineIndent()
	} else {
		output.Builder.WriteString(inline)
	}
}

func (output *Printer) writeCommaSeparatedList(l []*pg_query.Node) {
	output.writeListWithSeparator(l, ", ")
}

func (output *Printer) writeOptIndirection(l []*pg_query.Node) {
	for _, dn := range l {
		if dn.GetString_() != nil {
			output.Builder.WriteString(".")
			output.Builder.WriteString(quoteIdentifier(dn.GetString_().GetSval()))
		} else if dn.GetAStar() != nil {
			output.Builder.WriteString(".*")
		} else if dn.GetAIndices() != nil {
			n := dn.GetAIndices()
			output.Builder.WriteString("[")
			if n.Lidx != nil {
				output.writeNode(n.Lidx)
			}
			if n.IsSlice {
				output.Builder.WriteString(":")
			}
			if n.Uidx != nil {
				output.writeNode(n.Uidx)
			}
			output.Builder.WriteString("]")
		} else {
			warn("invalid indirection type")
		}
		output.writeNode(dn)
	}
}

func (output *Printer) writeSubqueryOp(l []*pg_query.Node) {
	if len(l) == 1 {
		sval := l[0].GetString_().GetSval()
		switch sval {
		case "~~":
			output.Builder.WriteString("LIKE")
			return
		case "!~~":
			output.Builder.WriteString("NOT LIKE")
			return
		case "~~*":
			output.Builder.WriteString("ILIKE")
			return
		case "!~~*":
			output.Builder.WriteString("NOT ILIKE")
			return
		}
		if isOp(sval) {
			output.Builder.WriteString(sval)
			return
		}
	}
	output.Builder.WriteString("OPERATOR(")
	output.writeAnyOperator(l)
	output.Builder.WriteString(")")
}

func (output *Printer) writeAnyOperator(l []*pg_query.Node) {
	if len(l) == 2 {
		output.Builder.WriteString(quoteIdentifier(l[0].GetString_().GetSval()))
		output.Builder.WriteString(".")
		output.Builder.WriteString(l[1].GetString_().GetSval())
	} else if len(l) == 1 {
		output.Builder.WriteString(l[0].GetString_().GetSval())
	} else {
		warn("unexpected operator")
	}
}

func (output *Printer) writeWithClause(node *pg_query.WithClause) {
	output.Builder.WriteString("WITH ")
	if node.Recursive {
		output.Builder.WriteString("RECURSIVE ")
	}
	for i, cte := range node.Ctes {
		output.Builder.WriteString("\n")
		output.writeIndent()
		output.writeNode(cte)
		if i != len(node.Ctes)-1 {
			output.Builder.WriteString(",")
		}
	}
	output.Builder.WriteString("\n")
}

// Frame option bit constants from PostgreSQL parsenodes.h
const (
	frameOptionNonDefault            = 0x00001
	frameOptionRange                 = 0x00002
	frameOptionRows                  = 0x00004
	frameOptionGroups                = 0x00008
	frameOptionBetween               = 0x00010
	frameOptionStartUnboundedPreceding = 0x00020
	frameOptionEndUnboundedPreceding   = 0x00040
	frameOptionStartUnboundedFollowing = 0x00080
	frameOptionEndUnboundedFollowing   = 0x00100
	frameOptionStartCurrentRow       = 0x00200
	frameOptionEndCurrentRow         = 0x00400
	frameOptionStartOffsetPreceding  = 0x00800
	frameOptionEndOffsetPreceding    = 0x01000
	frameOptionStartOffsetFollowing  = 0x02000
	frameOptionEndOffsetFollowing    = 0x04000
	frameOptionExcludeCurrentRow     = 0x08000
	frameOptionExcludeGroup          = 0x10000
	frameOptionExcludeTies           = 0x20000
)

func (output *Printer) writeWindowDef(def *pg_query.WindowDef) {
	if def.Name != "" {
		output.Builder.WriteString(def.Name)
		output.Builder.WriteString(" AS ")
	}
	output.Builder.WriteString("(")
	needSpace := false
	if def.Refname != "" {
		output.Builder.WriteString(def.Refname)
		needSpace = true
	}
	if len(def.PartitionClause) > 0 {
		if needSpace {
			output.Builder.WriteString(" ")
		}
		output.Builder.WriteString("PARTITION BY ")
		output.writeCommaSeparatedList(def.PartitionClause)
		needSpace = true
	}
	if len(def.OrderClause) > 0 {
		if needSpace {
			output.Builder.WriteString(" ")
		}
		output.Builder.WriteString("ORDER BY ")
		output.writeCommaSeparatedList(def.OrderClause)
		needSpace = true
	}
	fo := def.FrameOptions
	if fo&frameOptionNonDefault != 0 {
		if needSpace {
			output.Builder.WriteString(" ")
		}
		if fo&frameOptionRange != 0 {
			output.Builder.WriteString("RANGE ")
		} else if fo&frameOptionRows != 0 {
			output.Builder.WriteString("ROWS ")
		} else if fo&frameOptionGroups != 0 {
			output.Builder.WriteString("GROUPS ")
		}
		if fo&frameOptionBetween != 0 {
			output.Builder.WriteString("BETWEEN ")
		}
		output.writeFrameBound(fo, true, def.StartOffset)
		if fo&frameOptionBetween != 0 {
			output.Builder.WriteString(" AND ")
			output.writeFrameBound(fo, false, def.EndOffset)
		}
		if fo&frameOptionExcludeCurrentRow != 0 {
			output.Builder.WriteString(" EXCLUDE CURRENT ROW")
		} else if fo&frameOptionExcludeGroup != 0 {
			output.Builder.WriteString(" EXCLUDE GROUP")
		} else if fo&frameOptionExcludeTies != 0 {
			output.Builder.WriteString(" EXCLUDE TIES")
		}
	}
	output.Builder.WriteString(")")
}

func (output *Printer) writeFrameBound(fo int32, isStart bool, offset *pg_query.Node) {
	var unboundedPreceding, unboundedFollowing, currentRow, offsetPreceding, offsetFollowing int32
	if isStart {
		unboundedPreceding = frameOptionStartUnboundedPreceding
		unboundedFollowing = frameOptionStartUnboundedFollowing
		currentRow = frameOptionStartCurrentRow
		offsetPreceding = frameOptionStartOffsetPreceding
		offsetFollowing = frameOptionStartOffsetFollowing
	} else {
		unboundedPreceding = frameOptionEndUnboundedPreceding
		unboundedFollowing = frameOptionEndUnboundedFollowing
		currentRow = frameOptionEndCurrentRow
		offsetPreceding = frameOptionEndOffsetPreceding
		offsetFollowing = frameOptionEndOffsetFollowing
	}

	if fo&unboundedPreceding != 0 {
		output.Builder.WriteString("UNBOUNDED PRECEDING")
	} else if fo&unboundedFollowing != 0 {
		output.Builder.WriteString("UNBOUNDED FOLLOWING")
	} else if fo&currentRow != 0 {
		output.Builder.WriteString("CURRENT ROW")
	} else if fo&offsetPreceding != 0 {
		output.writeNode(offset)
		output.Builder.WriteString(" PRECEDING")
	} else if fo&offsetFollowing != 0 {
		output.writeNode(offset)
		output.Builder.WriteString(" FOLLOWING")
	}
}

func (output *Printer) writeListWithSeparator(l []*pg_query.Node, separator string) {
	for i, dn := range l {
		output.writeNode(dn)
		if i != len(l)-1 {
			output.Builder.WriteString(separator)
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
