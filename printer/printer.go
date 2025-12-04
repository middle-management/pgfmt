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
}

func (output *Printer) Print(node *pg_query.Node) {
	output.writeNode(node)
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
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ANY_SUBLINK:
		case pg_query.SubLinkType_ARRAY_SUBLINK:
			output.Builder.WriteString("ARRAY(")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_EXPR_SUBLINK:
			output.Builder.WriteString("(")
			output.writeNode(n.SubLink.Subselect)
			output.Builder.WriteString(")")
		case pg_query.SubLinkType_ROWCOMPARE_SUBLINK:
			fallthrough
		case pg_query.SubLinkType_MULTIEXPR_SUBLINK:
			fallthrough
		case pg_query.SubLinkType_CTE_SUBLINK:
			fallthrough
		default:
			panic("unexpected sublink type: " + n.SubLink.SubLinkType.String())
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
				output.Builder.WriteString("(")
				output.writeNode(n.AExpr.Lexpr)
				output.Builder.WriteString(")")
			}
			output.writeQualOp(n.AExpr.Name)
			if n.AExpr.Rexpr != nil {
				output.Builder.WriteString("(")
				output.writeNode(n.AExpr.Rexpr)
				output.Builder.WriteString(")")
			}
		case pg_query.A_Expr_Kind_AEXPR_OP_ANY:
		case pg_query.A_Expr_Kind_AEXPR_OP_ALL:
		case pg_query.A_Expr_Kind_AEXPR_DISTINCT:
		case pg_query.A_Expr_Kind_AEXPR_NOT_DISTINCT:
		case pg_query.A_Expr_Kind_AEXPR_NULLIF:
		case pg_query.A_Expr_Kind_AEXPR_IN:
		case pg_query.A_Expr_Kind_AEXPR_LIKE:
		case pg_query.A_Expr_Kind_AEXPR_ILIKE:
		case pg_query.A_Expr_Kind_AEXPR_SIMILAR:
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN:
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN:
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN_SYM:
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM:
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
		output.Builder.WriteString(" ")
		switch n.SortBy.SortbyDir {
		case pg_query.SortByDir_SORTBY_ASC:
			output.Builder.WriteString("ASC ")
		case pg_query.SortByDir_SORTBY_DESC:
			output.Builder.WriteString("DESC ")
		case pg_query.SortByDir_SORTBY_USING:
			output.Builder.WriteString("USING ")
			output.writeQualOp(n.SortBy.UseOp)
		case pg_query.SortByDir_SORTBY_DEFAULT:
		}
		switch n.SortBy.SortbyNulls {
		case pg_query.SortByNulls_SORTBY_NULLS_FIRST:
			output.Builder.WriteString("NULLS FIRST ")
		case pg_query.SortByNulls_SORTBY_NULLS_LAST:
			output.Builder.WriteString("NULLS LAST ")
		case pg_query.SortByNulls_SORTBY_NULLS_DEFAULT:
		}

	case *pg_query.Node_String_:
		output.Builder.WriteString(n.String_.Sval)

	case *pg_query.Node_ColumnRef:
		for _, f := range n.ColumnRef.Fields {
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
		output.Builder.WriteString(" ")
		output.Builder.WriteString("AS ")

		switch n.CommonTableExpr.Ctematerialized {
		case pg_query.CTEMaterialize_CTEMaterializeDefault:
			// no option
		case pg_query.CTEMaterialize_CTEMaterializeAlways:
			output.Builder.WriteString("MATERIALIZED ")
		case pg_query.CTEMaterialize_CTEMaterializeNever:
			output.Builder.WriteString("NOT MATERIALIZED ")
		}
		output.Builder.WriteString("(\n")
		output.writeNode(n.CommonTableExpr.Ctequery)
		output.Builder.WriteString("\n)\n")

	case *pg_query.Node_CoalesceExpr:
		output.Builder.WriteString("COALESCE(")
		output.writeCommaSeparatedList(n.CoalesceExpr.Args)
		output.Builder.WriteString(")")

	// case *pg_query.Node_TypeCast:

	case *pg_query.Node_IndexElem:
		if n.IndexElem.Name != "" {
			output.Builder.WriteString(n.IndexElem.Name)
			output.Builder.WriteString(" ")
		} else if n.IndexElem.Expr != nil {
			switch n.IndexElem.Expr.GetNode().(type) {
			case *pg_query.Node_FuncCall:
			case *pg_query.Node_SqlvalueFunction:
			case *pg_query.Node_TypeCast:
			case *pg_query.Node_CoalesceExpr:
			case *pg_query.Node_MinMaxExpr:
			case *pg_query.Node_XmlExpr:
			case *pg_query.Node_XmlSerialize:
				output.writeNode(n.IndexElem.Expr)
			default:
				output.Builder.WriteString("(")
				output.writeNode(n.IndexElem.Expr)
				output.Builder.WriteString(") ")
			}
		} else {
			panic("invalid index elem")
		}

		if len(n.IndexElem.Collation) > 0 {
			output.Builder.WriteString("COLLATE ")
			for _, o := range n.IndexElem.Collation {
				output.writeNode(o)
			}
			output.Builder.WriteString(" ")
		}

		if len(n.IndexElem.Opclass) > 0 {
			output.writeListWithSeparator(n.IndexElem.Opclass, ".")
			output.Builder.WriteString(" ")
		}

		switch n.IndexElem.Ordering {
		case pg_query.SortByDir_SORTBY_ASC:
			output.Builder.WriteString("ASC ")
		case pg_query.SortByDir_SORTBY_DESC:
			output.Builder.WriteString("DESC ")
		case pg_query.SortByDir_SORTBY_USING:
			panic("not allowed in CREATE INDEX")
		case pg_query.SortByDir_SORTBY_DEFAULT:
		}
		switch n.IndexElem.NullsOrdering {
		case pg_query.SortByNulls_SORTBY_NULLS_FIRST:
			output.Builder.WriteString("NULLS FIRST ")
		case pg_query.SortByNulls_SORTBY_NULLS_LAST:
			output.Builder.WriteString("NULLS LAST ")
		case pg_query.SortByNulls_SORTBY_NULLS_DEFAULT:
		}

		// TODO remove trailing space

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
		output.Builder.WriteString(" ")
		if n.IndexStmt.AccessMethod != "" {
			output.Builder.WriteString("USING ")
			output.Builder.WriteString(n.IndexStmt.AccessMethod) // TODO quote identifier
			output.Builder.WriteString(" ")
		}

		output.Builder.WriteString("(")
		output.writeCommaSeparatedList(n.IndexStmt.IndexParams)
		output.Builder.WriteString(") ")

		if len(n.IndexStmt.IndexIncludingParams) > 0 {
			output.Builder.WriteString("INCLUDE (")
			output.writeCommaSeparatedList(n.IndexStmt.IndexIncludingParams)
			output.Builder.WriteString(") ")
		}

		if len(n.IndexStmt.Options) > 0 {
			output.Builder.WriteString("WITH ")
			for _, o := range n.IndexStmt.Options {
				output.writeNode(o)
			}
			output.Builder.WriteString(" ")
		}

		if n.IndexStmt.TableSpace != "" {
			output.Builder.WriteString("TABLESPACE ")
			output.Builder.WriteString(n.IndexStmt.TableSpace) // TODO quote identifier
			output.Builder.WriteString(" ")
		}

		output.writeNode(n.IndexStmt.WhereClause)

		// TODO remove trailing space

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
		output.Builder.WriteString(" ")

		if len(n.InsertStmt.Cols) > 0 {
			output.Builder.WriteString("(")
			for i, c := range n.InsertStmt.Cols {
				output.Builder.WriteString(c.GetResTarget().Name)
				output.writeOptIndirection(c.GetResTarget().Indirection)
				if i != len(n.InsertStmt.Cols)-1 {
					output.Builder.WriteString(", ")
				}
			}
			output.Builder.WriteString(") ")
		}

		switch n.InsertStmt.Override {
		case pg_query.OverridingKind_OVERRIDING_NOT_SET:
			// do nothing
		case pg_query.OverridingKind_OVERRIDING_USER_VALUE:
			output.Builder.WriteString("OVERRIDING USER VALUE ")
		case pg_query.OverridingKind_OVERRIDING_SYSTEM_VALUE:
			output.Builder.WriteString("OVERRIDING SYSTEM VALUE ")
		}

		if n.InsertStmt.SelectStmt != nil {
			output.writeNode(n.InsertStmt.SelectStmt)
			output.Builder.WriteString(" ")
		} else {
			output.Builder.WriteString("DEFAULT VALUES ")
		}

	// TODO if (insert_stmt->onConflictClause != NULL)
	// {
	// 	deparseOnConflictClause(str, insert_stmt->onConflictClause);
	// 	appendStringInfoChar(str, ' ');
	// }

	// TODO if (list_length(insert_stmt->returningList) > 0)
	// {
	// 	appendStringInfoString(str, "RETURNING ");
	// 	deparseTargetList(str, insert_stmt->returningList);
	// }

	// TODO removeTrailingSpace(str);

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
			// TODO
			panic("TypeCast Aconst not implemented")
		default:
			output.writeNode(n.TypeCast.Arg)
			output.Builder.WriteString("::")
			output.writeTypeName(n.TypeCast.TypeName)
		}

	case nil:
		// nothing

	default:
		// if l, ok := node.GetNode()..(interface{ GetLocation() int32 }); ok {
		// 	fmt.Fprintf(os.Stderr, "unexpected node (at %d): %T\n", l.GetLocation(), n)
		// } else {
		fmt.Fprintf(os.Stderr, "unexpected node: %T\n", n)
		// }
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
					panic("invalid interval fields: " + strconv.Itoa(int(fields)))
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
	output.Builder.WriteString(" ")
	if stmt.Alias != nil {
		output.Builder.WriteString("AS ")
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
			if len(stmt.ValuesLists) > 0 {
				output.Builder.WriteString("ON (")
				output.writeCommaSeparatedList(stmt.ValuesLists)
				output.Builder.WriteString(")")
			}
		}

		output.Builder.WriteString("SELECT\n\t")
		for i, t := range stmt.TargetList {
			if stmt.DistinctClause != nil {
				output.Builder.WriteString("DISTINCT ")
				if len(stmt.DistinctClause) > 0 {
					output.Builder.WriteString("ON (")
					for _, dn := range stmt.DistinctClause {
						output.writeNode(dn)
					}
					output.Builder.WriteString(")")
				}
			}

			output.writeNode(t)
			if i != len(stmt.TargetList)-1 {
				output.Builder.WriteString(",\n\t")
			}
		}
		output.Builder.WriteString(" ")

		if stmt.IntoClause != nil {
			// TODO INTO
			panic("SELECT INTO not implemented")
		}

		if len(stmt.FromClause) > 0 {
			output.Builder.WriteString("\nFROM\n\t")
			output.writeListWithSeparator(stmt.FromClause, "")
			output.Builder.WriteString(" ")
		}
		if stmt.WhereClause != nil {
			output.Builder.WriteString("\nWHERE\n\t")
			output.writeNode(stmt.WhereClause)
			output.Builder.WriteString(" ")
		}

		if len(stmt.GroupClause) > 0 {
			output.Builder.WriteString("\nGROUP BY\n\t")
			output.writeCommaSeparatedList(stmt.GroupClause)
			output.Builder.WriteString(" ")
		}

		if stmt.HavingClause != nil {
			output.Builder.WriteString("HAVING ")
			output.writeNode(stmt.HavingClause)
			output.Builder.WriteString(" ")
		}

		if len(stmt.WindowClause) > 0 {
			output.Builder.WriteString("WINDOW ")
			output.writeCommaSeparatedList(stmt.WindowClause)
			output.Builder.WriteString(" ")
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
			panic("op")
		}

		if stmt.All {
			output.Builder.WriteString("ALL ")
		}

		output.Builder.WriteString("(")
		output.writeSelectStmt(stmt.Rarg)
		output.Builder.WriteString(")")
	}

	if len(stmt.SortClause) > 0 {
		output.Builder.WriteString("ORDER BY ")
		output.writeCommaSeparatedList(stmt.SortClause)
		output.Builder.WriteString(" ")
	}

	if stmt.LimitCount != nil {
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

		output.Builder.WriteString(" ")
		if stmt.LimitOption == pg_query.LimitOption_LIMIT_OPTION_WITH_TIES {
			output.Builder.WriteString("ROWS WITH TIES ")
		}
	}

	if stmt.LimitOffset != nil {
		output.Builder.WriteString("OFFSET ")
		output.writeNode(stmt.LimitOffset)
		output.Builder.WriteString(" ")
	}

	if len(stmt.LockingClause) > 0 {
		output.writeCommaSeparatedList(stmt.LockingClause)
		output.Builder.WriteString(" ")
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
			panic("invalid indirection type")
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
		panic("unexpected operator")
	}
}

func (output *Printer) writeWithClause(node *pg_query.WithClause) {
	output.Builder.WriteString("WITH ")
	if node.Recursive {
		output.Builder.WriteString("RECURSIVE ")
	}
	output.Builder.WriteString("\n")
	output.writeListWithSeparator(node.Ctes, ", ")
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
