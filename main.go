package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v2"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	result, err := pg_query.Parse(string(input))
	if err != nil {
		return err
	}

	for _, stmt := range result.Stmts {
		var output strings.Builder
		writeNode(&output, stmt.Stmt)
		fmt.Println(output.String())
	}

	return nil
}

func writeWithClause(output *strings.Builder, node *pg_query.WithClause) {
	output.WriteString("WITH ")
	if node.Recursive {
		output.WriteString("RECURSIVE ")
	}
	for _, lc := range node.Ctes {
		writeNode(output, lc)
	}
}

// https://github.com/pganalyze/libpg_query/blob/13-latest/src/pg_query_deparse.c
func writeNode(output *strings.Builder, node *pg_query.Node) {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_NamedArgExpr:
		output.WriteString(n.NamedArgExpr.Name)
		output.WriteString(" := ")
		writeNode(output, n.NamedArgExpr.Arg)
	case *pg_query.Node_Integer:
		output.WriteString(strconv.Itoa(int(n.Integer.Ival)))
	case *pg_query.Node_ParamRef:
		output.WriteString("$")
		output.WriteString(strconv.Itoa(int(n.ParamRef.Number)))

	case *pg_query.Node_AConst:
		writeNode(output, n.AConst.Val)
	case *pg_query.Node_AExpr:

		switch n.AExpr.Kind {
		case pg_query.A_Expr_Kind_AEXPR_OP:
			if n.AExpr.Lexpr != nil {
				output.WriteString("(")
				writeNode(output, n.AExpr.Lexpr)
				output.WriteString(")")
			}
			writeQualOp(output, n.AExpr.Name)
			if n.AExpr.Rexpr != nil {
				output.WriteString("(")
				writeNode(output, n.AExpr.Rexpr)
				output.WriteString(")")
			}
		case pg_query.A_Expr_Kind_AEXPR_OP_ANY:
		case pg_query.A_Expr_Kind_AEXPR_OP_ALL:
		case pg_query.A_Expr_Kind_AEXPR_DISTINCT:
		case pg_query.A_Expr_Kind_AEXPR_NOT_DISTINCT:
		case pg_query.A_Expr_Kind_AEXPR_NULLIF:
		case pg_query.A_Expr_Kind_AEXPR_OF:
		case pg_query.A_Expr_Kind_AEXPR_IN:
		case pg_query.A_Expr_Kind_AEXPR_LIKE:
		case pg_query.A_Expr_Kind_AEXPR_ILIKE:
		case pg_query.A_Expr_Kind_AEXPR_SIMILAR:
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN:
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN:
		case pg_query.A_Expr_Kind_AEXPR_BETWEEN_SYM:
		case pg_query.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM:
		case pg_query.A_Expr_Kind_AEXPR_PAREN:
		}

	case *pg_query.Node_FuncCall:
		for _, f := range n.FuncCall.Funcname {
			writeNode(output, f)
		}
		output.WriteString("(")
		if n.FuncCall.AggDistinct {
			output.WriteString("DISTINCT ")
		}
		if n.FuncCall.AggStar {
			output.WriteString("*")
		}
		for i, a := range n.FuncCall.Args {
			if n.FuncCall.FuncVariadic && i == len(n.FuncCall.Args)-1 {
				output.WriteString("VARIADIC ")
			}
			writeNode(output, a)

			if i != len(n.FuncCall.Args)-1 {
				output.WriteString(", ")
			}
		}
		output.WriteString(")")

	case *pg_query.Node_SortBy:
		writeNode(output, n.SortBy.Node)
		output.WriteString(" ")
		switch n.SortBy.SortbyDir {
		case pg_query.SortByDir_SORTBY_ASC:
			output.WriteString("ASC ")
		case pg_query.SortByDir_SORTBY_DESC:
			output.WriteString("DESC ")
		case pg_query.SortByDir_SORTBY_USING:
			output.WriteString("USING ")
			writeQualOp(output, n.SortBy.UseOp)
		case pg_query.SortByDir_SORTBY_DEFAULT:
		}
		switch n.SortBy.SortbyNulls {
		case pg_query.SortByNulls_SORTBY_NULLS_FIRST:
			output.WriteString("NULLS FIRST ")
		case pg_query.SortByNulls_SORTBY_NULLS_LAST:
			output.WriteString("NULLS LAST ")
		case pg_query.SortByNulls_SORTBY_NULLS_DEFAULT:
		}

	case *pg_query.Node_String_:
		output.WriteString(n.String_.Str)

	case *pg_query.Node_ColumnRef:
		for _, f := range n.ColumnRef.Fields {
			writeNode(output, f)
		}

	case *pg_query.Node_RangeVar:
		if n.RangeVar.Catalogname != "" {
			output.WriteString(n.RangeVar.Catalogname)
			output.WriteString(".")
		}
		if n.RangeVar.Schemaname != "" {
			output.WriteString(n.RangeVar.Schemaname)
			output.WriteString(".")
		}
		output.WriteString(n.RangeVar.Relname)
		output.WriteString(" ")
		if n.RangeVar.Alias != nil {
			output.WriteString("AS ")
			writeAlias(output, n.RangeVar.Alias)
		}

	case *pg_query.Node_RangeSubselect:
		if n.RangeSubselect.Lateral {
			output.WriteString("LATERAL ")
		}
		output.WriteString("(")
		writeNode(output, n.RangeSubselect.Subquery)
		output.WriteString(")")

		if n.RangeSubselect.Alias != nil {
			output.WriteString(" ")
			writeAlias(output, n.RangeSubselect.Alias)
		}

	case *pg_query.Node_ResTarget:
		writeNode(output, n.ResTarget.Val)
		if n.ResTarget.Name != "" {
			output.WriteString(" AS ")
			output.WriteString(n.ResTarget.Name)
		}

	case *pg_query.Node_BoolExpr:
		switch n.BoolExpr.Boolop {
		case pg_query.BoolExprType_AND_EXPR:
			for i, x := range n.BoolExpr.Args {
				output.WriteString("(")
				writeNode(output, x)
				output.WriteString(")")
				if i != len(n.BoolExpr.Args)-1 {
					output.WriteString(" AND ")
				}
			}

		case pg_query.BoolExprType_OR_EXPR:
			for i, x := range n.BoolExpr.Args {
				output.WriteString("(")
				writeNode(output, x)
				output.WriteString(")")
				if i != len(n.BoolExpr.Args)-1 {
					output.WriteString(" OR ")
				}
			}

		case pg_query.BoolExprType_NOT_EXPR:
			output.WriteString("NOT ")
			for _, x := range n.BoolExpr.Args {
				writeNode(output, x)
			}

		}

	case *pg_query.Node_SelectStmt:
		writeSelectStmt(output, n.SelectStmt)
	}
}

func writeSelectStmt(output *strings.Builder, stmt *pg_query.SelectStmt) {
	if stmt.WithClause != nil {
		writeWithClause(output, stmt.WithClause)
	}

	switch stmt.Op {
	case pg_query.SetOperation_SETOP_NONE:
		if stmt.ValuesLists != nil {
			output.WriteString("VALUES ")
			if len(stmt.ValuesLists) > 0 {
				output.WriteString("ON (")
				for i, dn := range stmt.ValuesLists {
					writeNode(output, dn)
					if i != len(stmt.ValuesLists)-1 {
						output.WriteString(", ")
					}
				}
				output.WriteString(")")
			}
		}

		output.WriteString("SELECT\n\t")
		for i, t := range stmt.TargetList {
			if stmt.DistinctClause != nil {
				output.WriteString("DISTINCT ")
				if len(stmt.DistinctClause) > 0 {
					output.WriteString("ON (")
					for _, dn := range stmt.DistinctClause {
						writeNode(output, dn)
					}
					output.WriteString(")")
				}
			}

			writeNode(output, t)
			if i != len(stmt.TargetList)-1 {
				output.WriteString(",\n\t")
			}
		}
		output.WriteString(" ")

		if stmt.IntoClause != nil {
			// TODO INTO
		}

		if len(stmt.FromClause) > 0 {
			output.WriteString("\nFROM\n\t")
			for _, x := range stmt.FromClause {
				writeNode(output, x)
			}
			output.WriteString(" ")
		}
		if stmt.WhereClause != nil {
			output.WriteString("\nWHERE\n\t")
			writeNode(output, stmt.WhereClause)
			output.WriteString(" ")
		}

		if len(stmt.GroupClause) > 0 {
			output.WriteString("\nGROUP BY\n\t")
			for i, x := range stmt.GroupClause {
				writeNode(output, x)
				if i != len(stmt.GroupClause)-1 {
					output.WriteString(", ")
				}
			}
			output.WriteString(" ")
		}

		if stmt.HavingClause != nil {
			output.WriteString("HAVING ")
			writeNode(output, stmt.HavingClause)
			output.WriteString(" ")
		}

		if len(stmt.WindowClause) > 0 {
			output.WriteString("WINDOW ")
			for i, x := range stmt.WindowClause {
				writeNode(output, x)
				if i != len(stmt.WindowClause)-1 {
					output.WriteString(", ")
				}
			}
			output.WriteString(" ")
		}
	case pg_query.SetOperation_SETOP_UNION, pg_query.SetOperation_SETOP_INTERSECT, pg_query.SetOperation_SETOP_EXCEPT:
		output.WriteString("(")
		writeSelectStmt(output, stmt.Larg)
		output.WriteString(")")

		switch stmt.Op {
		case pg_query.SetOperation_SETOP_UNION:
			output.WriteString(" UNION ")
		case pg_query.SetOperation_SETOP_INTERSECT:
			output.WriteString(" INTERSECT ")
		case pg_query.SetOperation_SETOP_EXCEPT:
			output.WriteString(" EXCEPT ")
		default:
			panic("op")
		}

		if stmt.All {
			output.WriteString("ALL ")
		}

		output.WriteString("(")
		writeSelectStmt(output, stmt.Rarg)
		output.WriteString(")")
	}

	if len(stmt.SortClause) > 0 {
		output.WriteString("ORDER BY ")
		for i, x := range stmt.SortClause {
			writeNode(output, x)
			if i != len(stmt.SortClause)-1 {
				output.WriteString(", ")
			}
		}
		output.WriteString(" ")
	}

	if stmt.LimitCount != nil {
		switch stmt.LimitOption {
		case pg_query.LimitOption_LIMIT_OPTION_COUNT:
			output.WriteString("LIMIT ")
		case pg_query.LimitOption_LIMIT_OPTION_WITH_TIES:
			output.WriteString("FETCH FIRST ")
		}

		if nullish := stmt.LimitCount.GetAConst().GetVal().GetNull(); nullish != nil {
			output.WriteString("ALL")
		} else {
			writeNode(output, stmt.LimitCount)
		}

		output.WriteString(" ")
		if stmt.LimitOption == pg_query.LimitOption_LIMIT_OPTION_WITH_TIES {
			output.WriteString("ROWS WITH TIES ")
		}
	}

	if stmt.LimitOffset != nil {
		output.WriteString("OFFSET ")
		writeNode(output, stmt.LimitOffset)
		output.WriteString(" ")
	}

	if len(stmt.LockingClause) > 0 {
		for i, lc := range stmt.LockingClause {
			writeNode(output, lc)
			if i != len(stmt.LockingClause)-1 {
				output.WriteString(", ")
			}
		}
		output.WriteString(" ")
	}
}

func writeQualOp(output *strings.Builder, n []*pg_query.Node) {
	if len(n) == 1 {
		writeNode(output, n[0])
	} else {
		output.WriteString("OPERATOR (")
		for _, x := range n {
			writeNode(output, x)
		}
		output.WriteString(")")
	}
}

func writeAlias(output *strings.Builder, a *pg_query.Alias) {
	output.WriteString(a.Aliasname)
	if len(a.Colnames) > 0 {
		output.WriteString("(")
		for _, c := range a.Colnames {
			writeNode(output, c)
		}
		output.WriteString(")")
	}
}
