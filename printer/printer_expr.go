package printer

import (
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
