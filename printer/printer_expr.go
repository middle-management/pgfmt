package printer

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

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

// writeAExprOperand writes an operand of a binary/comparison operator.
// IS NULL / IS TRUE bind looser than comparison operators, so those tests
// (and nested boolean/comparison expressions) need parentheses here.
func (output *Printer) writeAExprOperand(node *pg_query.Node) {
	switch node.GetNode().(type) {
	case *pg_query.Node_NullTest, *pg_query.Node_BooleanTest:
		output.Builder.WriteString("(")
		output.writeNode(node)
		output.Builder.WriteString(")")
	default:
		output.writeExprWithParensIfNeeded(node)
	}
}

// keyValuePairFuncs are functions taking alternating key/value arguments.
// Calls with two or more pairs are formatted with one pair per line.
var keyValuePairFuncs = map[string]bool{
	"json_build_object":  true,
	"jsonb_build_object": true,
}

// isKeyValuePairCall reports whether the function call should be formatted
// with one key/value pair per line.
func isKeyValuePairCall(fc *pg_query.FuncCall) bool {
	if len(fc.Args) < 4 || fc.AggStar || fc.AggDistinct || fc.FuncVariadic || len(fc.AggOrder) > 0 {
		return false
	}
	if len(fc.Funcname) == 0 {
		return false
	}
	name := fc.Funcname[len(fc.Funcname)-1].GetString_().GetSval()
	return keyValuePairFuncs[name]
}

// containsKeyValuePairCall reports whether the expression renders as a
// multi-line key/value call, possibly wrapped in casts, named arguments or
// other function calls.
func containsKeyValuePairCall(node *pg_query.Node) bool {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_FuncCall:
		if isKeyValuePairCall(n.FuncCall) {
			return true
		}
		for _, a := range n.FuncCall.Args {
			if containsKeyValuePairCall(a) {
				return true
			}
		}
	case *pg_query.Node_NamedArgExpr:
		return containsKeyValuePairCall(n.NamedArgExpr.Arg)
	case *pg_query.Node_TypeCast:
		return containsKeyValuePairCall(n.TypeCast.Arg)
	case *pg_query.Node_CoalesceExpr:
		for _, a := range n.CoalesceExpr.Args {
			if containsKeyValuePairCall(a) {
				return true
			}
		}
	case *pg_query.Node_JsonObjectConstructor:
		return len(n.JsonObjectConstructor.Exprs) >= 2
	}
	return false
}

// deparseExprFallback renders an arbitrary expression node by deparsing a
// synthetic single-target SELECT and stripping the keyword. Used for
// expression nodes the printer does not handle explicitly.
func deparseExprFallback(node *pg_query.Node) (string, bool) {
	deparsed, err := pgDeparse(&pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{{
			Stmt: &pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: &pg_query.SelectStmt{
				TargetList: []*pg_query.Node{{
					Node: &pg_query.Node_ResTarget{ResTarget: &pg_query.ResTarget{Val: node}},
				}},
			}}},
		}},
	})
	if err != nil {
		return "", false
	}
	return strings.CutPrefix(deparsed, "SELECT ")
}

// deparseRangeFallback renders an arbitrary FROM-clause item by deparsing a
// synthetic SELECT and stripping the prefix. Used for range nodes the printer
// does not handle explicitly (JSON_TABLE, XMLTABLE, ...); without it these
// would hit the whole-statement fallback and paste the entire statement into
// the FROM clause.
func deparseRangeFallback(node *pg_query.Node) (string, bool) {
	deparsed, err := pgDeparse(&pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{{
			Stmt: &pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: &pg_query.SelectStmt{
				TargetList: []*pg_query.Node{{
					Node: &pg_query.Node_ResTarget{ResTarget: &pg_query.ResTarget{
						Val: &pg_query.Node{Node: &pg_query.Node_ColumnRef{ColumnRef: &pg_query.ColumnRef{
							Fields: []*pg_query.Node{{Node: &pg_query.Node_AStar{AStar: &pg_query.A_Star{}}}},
						}}},
					}},
				}},
				FromClause: []*pg_query.Node{node},
			}}},
		}},
	})
	if err != nil {
		return "", false
	}
	return strings.CutPrefix(deparsed, "SELECT * FROM ")
}

// breaksArgsForKeyValuePair reports whether the function call should place
// each argument on its own line because one of them is a multi-line
// key/value call.
func breaksArgsForKeyValuePair(fc *pg_query.FuncCall) bool {
	if len(fc.AggOrder) > 0 || fc.AggWithinGroup {
		return false
	}
	for _, a := range fc.Args {
		if containsKeyValuePairCall(a) {
			return true
		}
	}
	return false
}

// writeKeyValuePairArgs writes alternating key/value arguments with one pair
// per line and the closing parenthesis (written by the caller) on its own line.
func (output *Printer) writeKeyValuePairArgs(args []*pg_query.Node) {
	output.indent++
	for i := 0; i < len(args); i += 2 {
		output.writeNewlineIndent()
		output.writeNode(args[i])
		if i+1 < len(args) {
			output.Builder.WriteString(", ")
			output.writeNode(args[i+1])
		}
		if i+2 < len(args) {
			output.Builder.WriteString(",")
		}
	}
	output.indent--
	output.writeNewlineIndent()
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
