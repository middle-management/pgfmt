package printer

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// writePartitionBound emits a partition bound clause for
// CREATE TABLE ... PARTITION OF / ALTER TABLE ... ATTACH PARTITION:
// FOR VALUES IN (...) | FOR VALUES FROM (...) TO (...) |
// FOR VALUES WITH (MODULUS m, REMAINDER r) | DEFAULT
func (output *Printer) writePartitionBound(b *pg_query.PartitionBoundSpec) {
	if b.IsDefault {
		output.Builder.WriteString(" DEFAULT")
		return
	}
	switch b.Strategy {
	case "l":
		output.Builder.WriteString(" FOR VALUES IN (")
		output.writeCommaSeparatedList(b.Listdatums)
		output.Builder.WriteString(")")
	case "r":
		output.Builder.WriteString(" FOR VALUES FROM (")
		output.writeCommaSeparatedList(b.Lowerdatums)
		output.Builder.WriteString(") TO (")
		output.writeCommaSeparatedList(b.Upperdatums)
		output.Builder.WriteString(")")
	case "h":
		fmt.Fprintf(output.Builder, " FOR VALUES WITH (MODULUS %d, REMAINDER %d)", b.Modulus, b.Remainder)
	default:
		warn("unsupported partition bound strategy: %q", b.Strategy)
	}
}

// writePartitionSpec emits a PARTITION BY clause.
func (output *Printer) writePartitionSpec(s *pg_query.PartitionSpec) {
	output.Builder.WriteString(" PARTITION BY ")
	switch s.Strategy {
	case pg_query.PartitionStrategy_PARTITION_STRATEGY_RANGE:
		output.Builder.WriteString("RANGE")
	case pg_query.PartitionStrategy_PARTITION_STRATEGY_LIST:
		output.Builder.WriteString("LIST")
	case pg_query.PartitionStrategy_PARTITION_STRATEGY_HASH:
		output.Builder.WriteString("HASH")
	default:
		warn("unsupported partition strategy: %s", s.Strategy.String())
	}
	output.Builder.WriteString(" (")
	output.writeCommaSeparatedList(s.PartParams)
	output.Builder.WriteString(")")
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
