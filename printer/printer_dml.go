package printer

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

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

// setOpBranch represents one branch in a flattened set operation chain.
// The first branch has op/all unset (it's the leftmost SELECT).
type setOpBranch struct {
	stmt *pg_query.SelectStmt
	op   pg_query.SetOperation // the operator BEFORE this branch
	all  bool
}

// collectSetOpBranches flattens a chain of same-type set operations into a
// list of branches. For example, A UNION B UNION C (parsed as (A UNION B) UNION C)
// becomes [A, B, C]. When the operation type changes (e.g., UNION vs EXCEPT),
// the differing subtree is kept as-is (not flattened further).
func collectSetOpBranches(stmt *pg_query.SelectStmt) []setOpBranch {
	var branches []setOpBranch
	var collect func(s *pg_query.SelectStmt, op pg_query.SetOperation, all bool)
	collect = func(s *pg_query.SelectStmt, op pg_query.SetOperation, all bool) {
		// If the left side is the same set op type, flatten it
		if s.Op != pg_query.SetOperation_SETOP_NONE && s.Larg != nil &&
			(op == pg_query.SetOperation_SETOP_NONE || (s.Op == op && s.All == all)) {
			collect(s.Larg, s.Op, s.All)
			branches = append(branches, setOpBranch{stmt: s.Rarg, op: s.Op, all: s.All})
		} else {
			branches = append(branches, setOpBranch{stmt: s, op: op, all: all})
		}
	}
	collect(stmt, pg_query.SetOperation_SETOP_NONE, false)
	return branches
}

func (output *Printer) writeSetOpKeyword(op pg_query.SetOperation, all bool) {
	switch op {
	case pg_query.SetOperation_SETOP_UNION:
		output.Builder.WriteString("UNION")
	case pg_query.SetOperation_SETOP_INTERSECT:
		output.Builder.WriteString("INTERSECT")
	case pg_query.SetOperation_SETOP_EXCEPT:
		output.Builder.WriteString("EXCEPT")
	}
	if all {
		output.Builder.WriteString(" ALL")
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
		output.indent++
		output.writeNewlineIndent()
		for i, t := range stmt.TargetList {
			output.writeNode(t)
			if i != len(stmt.TargetList)-1 {
				output.Builder.WriteString(",")
				output.writeNewlineIndent()
			}
		}
		output.indent--

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
		branches := collectSetOpBranches(stmt)
		for i, branch := range branches {
			if i > 0 {
				output.writeNewlineIndent()
				output.writeSetOpKeyword(branch.op, branch.all)
				output.writeNewlineIndent()
			}
			// Wrap in parens only if this branch is itself a different set operation
			if branch.stmt.Op != pg_query.SetOperation_SETOP_NONE {
				output.Builder.WriteString("(")
				output.indent++
				output.writeNewlineIndent()
				output.writeSelectStmt(branch.stmt)
				output.indent--
				output.writeNewlineIndent()
				output.Builder.WriteString(")")
			} else {
				output.writeSelectStmt(branch.stmt)
			}
		}
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
	frameOptionNonDefault              = 0x00001
	frameOptionRange                   = 0x00002
	frameOptionRows                    = 0x00004
	frameOptionGroups                  = 0x00008
	frameOptionBetween                 = 0x00010
	frameOptionStartUnboundedPreceding = 0x00020
	frameOptionEndUnboundedPreceding   = 0x00040
	frameOptionStartUnboundedFollowing = 0x00080
	frameOptionEndUnboundedFollowing   = 0x00100
	frameOptionStartCurrentRow         = 0x00200
	frameOptionEndCurrentRow           = 0x00400
	frameOptionStartOffsetPreceding    = 0x00800
	frameOptionEndOffsetPreceding      = 0x01000
	frameOptionStartOffsetFollowing    = 0x02000
	frameOptionEndOffsetFollowing      = 0x04000
	frameOptionExcludeCurrentRow       = 0x08000
	frameOptionExcludeGroup            = 0x10000
	frameOptionExcludeTies             = 0x20000
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
