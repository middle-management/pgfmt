package printer

// https://github.com/pganalyze/libpg_query/blob/13-latest/src/pg_query_deparse.c

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v5"
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
	Builder *strings.Builder
	indent  int // current indentation level
}

func (output *Printer) Print(node *pg_query.Node) {
	output.writeNode(node)
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
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ALL_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" ALL (")
			output.writeNode(n.SubLink.Subselect)
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
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ROWCOMPARE_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" (")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_MULTIEXPR_SUBLINK:
			output.writeNode(n.SubLink.Testexpr)
			output.Builder.WriteString(" ")
			output.writeSubqueryOp(n.SubLink.OperName)
			output.Builder.WriteString(" (")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ARRAY_SUBLINK:
			output.Builder.WriteString("ARRAY(")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_EXPR_SUBLINK:
			output.Builder.WriteString("(")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_CTE_SUBLINK:
			output.Builder.WriteString("/* UNSUPPORTED: CTE sublink */")
		default:
			fmt.Fprintf(os.Stderr, "unsupported sublink type: %s\n", n.SubLink.SubLinkType.String())
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
		output.Builder.WriteString(")")

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
				*pg_query.Node_TypeCast,
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
			fmt.Fprintf(os.Stderr, "invalid index elem\n")
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
			fmt.Fprintf(os.Stderr, "SORTBY_USING not allowed in CREATE INDEX\n")
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
		output.writeNode(n.RangeSubselect.Subquery)
		output.Builder.WriteString(")")

		if n.RangeSubselect.Alias != nil {
			output.Builder.WriteString(" ")
			output.writeAlias(n.RangeSubselect.Alias)
		}

	case *pg_query.Node_JoinExpr:
		output.writeNode(n.JoinExpr.Larg)
		switch n.JoinExpr.Jointype {
		case pg_query.JoinType_JOIN_INNER:
			if n.JoinExpr.IsNatural {
				output.Builder.WriteString(" NATURAL JOIN ")
			} else if n.JoinExpr.Quals != nil {
				output.Builder.WriteString("\n\tJOIN ")
			} else {
				output.Builder.WriteString("\n\tCROSS JOIN ")
			}
		case pg_query.JoinType_JOIN_LEFT:
			output.Builder.WriteString("\n\tLEFT JOIN ")
		case pg_query.JoinType_JOIN_FULL:
			output.Builder.WriteString("\n\tFULL JOIN ")
		case pg_query.JoinType_JOIN_RIGHT:
			output.Builder.WriteString("\n\tRIGHT JOIN ")
		default:
			output.Builder.WriteString("\n\tJOIN ")
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
				output.Builder.WriteString("(")
				output.writeNode(x)
				output.Builder.WriteString(")")
				if i != len(n.BoolExpr.Args)-1 {
					output.Builder.WriteString(" AND ")
				}
			}

		case pg_query.BoolExprType_OR_EXPR:
			for i, x := range n.BoolExpr.Args {
				output.Builder.WriteString("(")
				output.writeNode(x)
				output.Builder.WriteString(")")
				if i != len(n.BoolExpr.Args)-1 {
					output.Builder.WriteString(" OR ")
				}
			}

		case pg_query.BoolExprType_NOT_EXPR:
			output.Builder.WriteString("NOT ")
			for _, x := range n.BoolExpr.Args {
				output.writeNode(x)
			}

		}

	case *pg_query.Node_SelectStmt:
		output.writeSelectStmt(n.SelectStmt)

	case *pg_query.Node_InsertStmt:
		if n.InsertStmt.WithClause != nil {
			output.writeWithClause(n.InsertStmt.WithClause)
		}

		output.Builder.WriteString("INSERT INTO ")
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
			output.Builder.WriteString("\n")
			output.writeNode(n.InsertStmt.SelectStmt)
		} else {
			output.Builder.WriteString("\nDEFAULT VALUES")
		}

		if n.InsertStmt.OnConflictClause != nil {
			output.Builder.WriteString("\n")
			output.writeOnConflictClause(n.InsertStmt.OnConflictClause)
		}

		if len(n.InsertStmt.ReturningList) > 0 {
			output.Builder.WriteString("\nRETURNING\n\t")
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
		output.Builder.WriteString(n.ColumnDef.Colname)
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
		default:
			fmt.Fprintf(os.Stderr, "unsupported constraint type: %s\n", n.Constraint.Contype.String())
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
			fmt.Fprintf(os.Stderr, "unsupported alter table cmd: %s\n", n.AlterTableCmd.Subtype.String())
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
			fmt.Fprintf(os.Stderr, "unsupported drop type: %s\n", n.DropStmt.RemoveType.String())
		}
		if n.DropStmt.MissingOk {
			output.Builder.WriteString("IF EXISTS ")
		}
		for i, obj := range n.DropStmt.Objects {
			if l := obj.GetList(); l != nil {
				output.writeListWithSeparator(l.Items, ".")
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
			for _, opt := range n.TransactionStmt.Options {
				if s := opt.GetDefElem(); s != nil && s.Defname == "savepoint_name" {
					output.Builder.WriteString(s.Arg.GetString_().GetSval())
				}
			}
		case pg_query.TransactionStmtKind_TRANS_STMT_RELEASE:
			output.Builder.WriteString("RELEASE SAVEPOINT ")
			for _, opt := range n.TransactionStmt.Options {
				if s := opt.GetDefElem(); s != nil && s.Defname == "savepoint_name" {
					output.Builder.WriteString(s.Arg.GetString_().GetSval())
				}
			}
		case pg_query.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO:
			output.Builder.WriteString("ROLLBACK TO SAVEPOINT ")
			for _, opt := range n.TransactionStmt.Options {
				if s := opt.GetDefElem(); s != nil && s.Defname == "savepoint_name" {
					output.Builder.WriteString(s.Arg.GetString_().GetSval())
				}
			}
		default:
			fmt.Fprintf(os.Stderr, "unsupported transaction kind: %s\n", n.TransactionStmt.Kind.String())
		}

	case *pg_query.Node_DefElem:
		output.Builder.WriteString(n.DefElem.Defname)
		if n.DefElem.Arg != nil {
			output.Builder.WriteString(" = ")
			output.writeNode(n.DefElem.Arg)
		}

	case nil:
		// nothing

	default:
		fmt.Fprintf(os.Stderr, "unexpected node: %T\n", n)
	}
}

func (output *Printer) writeExprWithParensIfNeeded(node *pg_query.Node) {
	switch node.GetNode().(type) {
	case *pg_query.Node_AExpr, *pg_query.Node_BoolExpr:
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

func (output *Printer) writeTypeName(stmt *pg_query.TypeName) {
	var skipTypmods bool
	if stmt.Setof {
		output.Builder.WriteString("SETOF ")
	}
	if len(stmt.Names) == 2 && stmt.Names[0].String() == "pg_catalog" {
		switch stmt.Names[1].String() {
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
			output.Builder.WriteString(stmt.Names[1].String())
		case "timetz", "timestamptz":
			output.Builder.WriteString(stmt.Names[1].String())
			if len(stmt.Typmods) > 0 {
				output.Builder.WriteString("(")
				output.writeCommaSeparatedList(stmt.Typmods)
				output.Builder.WriteString(")")
			}
			output.Builder.WriteString("with time zone")
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
					fmt.Fprintf(os.Stderr, "invalid interval fields: %d\n", fields)
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
			output.Builder.WriteString(stmt.Names[1].String())

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
			output.Builder.WriteString(" DISTINCT")
			if len(stmt.DistinctClause) > 0 {
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
			output.writeNewlineIndent()
			output.Builder.WriteString("\t")
			output.writeNode(stmt.WhereClause)
		}

		if len(stmt.GroupClause) > 0 {
			output.writeNewlineIndent()
			output.Builder.WriteString("GROUP BY")
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

		switch stmt.Op {
		case pg_query.SetOperation_SETOP_UNION:
			output.Builder.WriteString(" UNION ")
		case pg_query.SetOperation_SETOP_INTERSECT:
			output.Builder.WriteString(" INTERSECT ")
		case pg_query.SetOperation_SETOP_EXCEPT:
			output.Builder.WriteString(" EXCEPT ")
		default:
			fmt.Fprintf(os.Stderr, "unexpected set operation\n")
		}

		if stmt.All {
			output.Builder.WriteString("ALL ")
		}

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
			fmt.Fprintf(os.Stderr, "invalid indirection type\n")
		}
		output.writeNode(dn)
	}
}

func (output *Printer) writeSubqueryOp(l []*pg_query.Node) {
	if len(l) == 1 && l[0].String() == "~~" {
		output.Builder.WriteString("LIKE")
	} else if len(l) == 1 && l[0].String() == "!~~" {
		output.Builder.WriteString("NOT LIKE")
	} else if len(l) == 1 && l[0].String() == "~~*" {
		output.Builder.WriteString("ILIKE")
	} else if len(l) == 1 && l[0].String() == "!~~*" {
		output.Builder.WriteString("NOT ILIKE")
	} else if len(l) == 1 && isOp(l[0].String()) {
		output.Builder.WriteString(l[0].String())
	} else {
		output.Builder.WriteString("OPERATOR(")
		output.writeAnyOperator(l)
		output.Builder.WriteString(")")
	}
}

func (output *Printer) writeAnyOperator(l []*pg_query.Node) {
	if len(l) == 2 {
		output.Builder.WriteString(quoteIdentifier(l[0].String()))
		output.Builder.WriteString(".")
		output.Builder.WriteString(l[1].String())
	} else if len(l) == 1 {
		output.Builder.WriteString(l[0].String())
	} else {
		fmt.Fprintf(os.Stderr, "unexpected operator\n")
	}
}

func (output *Printer) writeWithClause(node *pg_query.WithClause) {
	output.Builder.WriteString("WITH ")
	if node.Recursive {
		output.Builder.WriteString("RECURSIVE ")
	}
	output.indent++
	for i, cte := range node.Ctes {
		output.writeNewlineIndent()
		output.writeNode(cte)
		if i != len(node.Ctes)-1 {
			output.Builder.WriteString(",")
		}
	}
	output.indent--
	output.Builder.WriteString("\n")
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
	return `"` + strings.Replace(name, `"`, `""`, -1) + `"`
}
