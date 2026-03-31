package printer

import (
	"encoding/json"
	"strings"
)

// Top-level parse result: [{"PLpgSQL_function": {...}}]
type plFunctionW struct {
	F plFunction `json:"PLpgSQL_function"`
}

type plFunction struct {
	Action plStmtBlockW `json:"action"`
	Datums []plDatum    `json:"datums"`
}

type plStmtBlockW struct {
	B plStmtBlock `json:"PLpgSQL_stmt_block"`
}

// Expressions

type plExpr struct {
	Query     string `json:"query"`
	ParseMode int    `json:"parseMode"`
}

// plExprW wraps {"PLpgSQL_expr": {...}}.
type plExprW struct {
	E plExpr `json:"PLpgSQL_expr"`
}

func (w *plExprW) query() string {
	if w == nil {
		return ""
	}
	return w.E.Query
}

// Types

type plType struct {
	TypeName string `json:"typname"`
}

type plDataType struct {
	T plType `json:"PLpgSQL_type"`
}

// Datums

type plVar struct {
	RefName    string      `json:"refname"`
	LineNo     int         `json:"lineno"`
	IsConst    bool        `json:"isconst"`
	NotNull    bool        `json:"notnull"`
	DataType   *plDataType `json:"datatype"`
	DefaultVal *plExprW    `json:"default_val"`
}

type plRec struct {
	RefName string `json:"refname"`
	LineNo  int    `json:"lineno"`
	Dno     int    `json:"dno"`
}

type plRowField struct {
	Name  string `json:"name"`
	VarNo int    `json:"varno"`
}

type plRow struct {
	RefName string       `json:"refname"`
	LineNo  int          `json:"lineno"`
	Fields  []plRowField `json:"fields"`
}

// plDatum is a tagged union for PLpgSQL_var | PLpgSQL_rec | PLpgSQL_row.
type plDatum struct {
	Var *plVar
	Rec *plRec
	Row *plRow
}

func (d *plDatum) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["PLpgSQL_var"]; ok {
		d.Var = &plVar{}
		return json.Unmarshal(v, d.Var)
	}
	if v, ok := raw["PLpgSQL_rec"]; ok {
		d.Rec = &plRec{}
		return json.Unmarshal(v, d.Rec)
	}
	if v, ok := raw["PLpgSQL_row"]; ok {
		d.Row = &plRow{}
		return json.Unmarshal(v, d.Row)
	}
	return nil
}

func (d *plDatum) name() string {
	if d == nil {
		return ""
	}
	if d.Var != nil {
		return d.Var.RefName
	}
	if d.Rec != nil {
		return d.Rec.RefName
	}
	if d.Row != nil && len(d.Row.Fields) > 0 {
		return d.Row.Fields[0].Name
	}
	return ""
}

func (d *plDatum) fieldNames() string {
	if d == nil {
		return ""
	}
	if d.Row != nil {
		var names []string
		for _, f := range d.Row.Fields {
			names = append(names, f.Name)
		}
		return strings.Join(names, ", ")
	}
	return d.name()
}

// Statements

type plStmtBlock struct {
	LineNo     int          `json:"lineno"`
	Label      string       `json:"label"`
	Body       []plStmt     `json:"body"`
	Exceptions *plExcBlockW `json:"exceptions"`
}

type plStmtAssign struct {
	LineNo int     `json:"lineno"`
	VarNo  int     `json:"varno"`
	Expr   plExprW `json:"expr"`
}

type plStmtIf struct {
	LineNo    int          `json:"lineno"`
	Cond      plExprW      `json:"cond"`
	ThenBody  []plStmt     `json:"then_body"`
	ElsIfList []plElsIfW   `json:"elsif_list"`
	ElseBody  []plStmt     `json:"else_body"`
}

type plElsIfW struct {
	E plElsIf `json:"PLpgSQL_if_elsif"`
}

type plElsIf struct {
	LineNo int      `json:"lineno"`
	Cond   plExprW  `json:"cond"`
	Stmts  []plStmt `json:"stmts"`
}

type plStmtCase struct {
	LineNo       int            `json:"lineno"`
	TExpr        *plExprW       `json:"t_expr"`
	TVarNo       int            `json:"t_varno"`
	CaseWhenList []plCaseWhenW  `json:"case_when_list"`
	HaveElse     bool           `json:"have_else"`
	ElseStmts    []plStmt       `json:"else_stmts"`
}

type plCaseWhenW struct {
	W plCaseWhen `json:"PLpgSQL_case_when"`
}

type plCaseWhen struct {
	LineNo int      `json:"lineno"`
	Expr   plExprW  `json:"expr"`
	Stmts  []plStmt `json:"stmts"`
}

type plStmtLoop struct {
	LineNo int      `json:"lineno"`
	Label  string   `json:"label"`
	Body   []plStmt `json:"body"`
}

type plStmtWhile struct {
	LineNo int      `json:"lineno"`
	Cond   plExprW  `json:"cond"`
	Body   []plStmt `json:"body"`
}

type plStmtForI struct {
	LineNo  int      `json:"lineno"`
	Var     plDatum  `json:"var"`
	Lower   plExprW  `json:"lower"`
	Upper   plExprW  `json:"upper"`
	Step    *plExprW `json:"step"`
	Reverse bool     `json:"reverse"`
	Body    []plStmt `json:"body"`
}

type plStmtForS struct {
	LineNo int      `json:"lineno"`
	Var    plDatum  `json:"var"`
	Query  plExprW  `json:"query"`
	Body   []plStmt `json:"body"`
}

type plStmtForEachA struct {
	LineNo int      `json:"lineno"`
	VarNo  int      `json:"varno"`
	Expr   plExprW  `json:"expr"`
	Body   []plStmt `json:"body"`
}

type plStmtExit struct {
	LineNo int      `json:"lineno"`
	IsExit bool     `json:"is_exit"`
	Label  string   `json:"label"`
	Cond   *plExprW `json:"cond"`
}

type plStmtReturn struct {
	LineNo int      `json:"lineno"`
	Expr   *plExprW `json:"expr"`
}

type plStmtReturnNext struct {
	LineNo int      `json:"lineno"`
	Expr   *plExprW `json:"expr"`
}

type plStmtReturnQuery struct {
	LineNo   int      `json:"lineno"`
	Query    *plExprW `json:"query"`
	DynQuery *plExprW `json:"dynquery"`
}

type plStmtRaise struct {
	LineNo    int       `json:"lineno"`
	ElogLevel int       `json:"elog_level"`
	Message   string    `json:"message"`
	Params    []plExprW `json:"params"`
}

type plStmtExecSQL struct {
	LineNo  int      `json:"lineno"`
	SQLStmt plExprW  `json:"sqlstmt"`
	Into    bool     `json:"into"`
	Strict  bool     `json:"strict"`
	Target  *plDatum `json:"target"`
}

type plStmtPerform struct {
	LineNo int     `json:"lineno"`
	Expr   plExprW `json:"expr"`
}

type plStmtDynExecute struct {
	LineNo int       `json:"lineno"`
	Query  plExprW   `json:"query"`
	Into   bool      `json:"into"`
	Strict bool      `json:"strict"`
	Target *plDatum  `json:"target"`
	Params []plExprW `json:"params"`
}

// Exception handling

type plExcBlockW struct {
	B plExcBlock `json:"PLpgSQL_exception_block"`
}

type plExcBlock struct {
	ExcList []plExceptionW `json:"exc_list"`
}

type plExceptionW struct {
	E plException `json:"PLpgSQL_exception"`
}

type plException struct {
	Conditions []plConditionW `json:"conditions"`
	Action     []plStmt       `json:"action"`
}

type plConditionW struct {
	C plCondition `json:"PLpgSQL_condition"`
}

type plCondition struct {
	CondName string `json:"condname"`
}

// plStmt is a tagged union for all PL/pgSQL statement types.
type plStmt struct {
	Assign      *plStmtAssign
	If          *plStmtIf
	Case        *plStmtCase
	Loop        *plStmtLoop
	While       *plStmtWhile
	ForI        *plStmtForI
	ForS        *plStmtForS
	ForEachA    *plStmtForEachA
	Exit        *plStmtExit
	Return      *plStmtReturn
	ReturnNext  *plStmtReturnNext
	ReturnQuery *plStmtReturnQuery
	Raise       *plStmtRaise
	ExecSQL     *plStmtExecSQL
	Perform     *plStmtPerform
	DynExecute  *plStmtDynExecute
	Block       *plStmtBlock
}

func (s *plStmt) lineNo() int {
	switch {
	case s.Assign != nil:
		return s.Assign.LineNo
	case s.If != nil:
		return s.If.LineNo
	case s.Case != nil:
		return s.Case.LineNo
	case s.Loop != nil:
		return s.Loop.LineNo
	case s.While != nil:
		return s.While.LineNo
	case s.ForI != nil:
		return s.ForI.LineNo
	case s.ForS != nil:
		return s.ForS.LineNo
	case s.ForEachA != nil:
		return s.ForEachA.LineNo
	case s.Exit != nil:
		return s.Exit.LineNo
	case s.Return != nil:
		return s.Return.LineNo
	case s.ReturnNext != nil:
		return s.ReturnNext.LineNo
	case s.ReturnQuery != nil:
		return s.ReturnQuery.LineNo
	case s.Raise != nil:
		return s.Raise.LineNo
	case s.ExecSQL != nil:
		return s.ExecSQL.LineNo
	case s.Perform != nil:
		return s.Perform.LineNo
	case s.DynExecute != nil:
		return s.DynExecute.LineNo
	case s.Block != nil:
		return s.Block.LineNo
	}
	return 0
}

func (s *plStmt) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, val := range raw {
		switch key {
		case "PLpgSQL_stmt_assign":
			s.Assign = &plStmtAssign{}
			return json.Unmarshal(val, s.Assign)
		case "PLpgSQL_stmt_if":
			s.If = &plStmtIf{}
			return json.Unmarshal(val, s.If)
		case "PLpgSQL_stmt_case":
			s.Case = &plStmtCase{}
			return json.Unmarshal(val, s.Case)
		case "PLpgSQL_stmt_loop":
			s.Loop = &plStmtLoop{}
			return json.Unmarshal(val, s.Loop)
		case "PLpgSQL_stmt_while":
			s.While = &plStmtWhile{}
			return json.Unmarshal(val, s.While)
		case "PLpgSQL_stmt_fori":
			s.ForI = &plStmtForI{}
			return json.Unmarshal(val, s.ForI)
		case "PLpgSQL_stmt_fors":
			s.ForS = &plStmtForS{}
			return json.Unmarshal(val, s.ForS)
		case "PLpgSQL_stmt_foreach_a":
			s.ForEachA = &plStmtForEachA{}
			return json.Unmarshal(val, s.ForEachA)
		case "PLpgSQL_stmt_exit":
			s.Exit = &plStmtExit{}
			return json.Unmarshal(val, s.Exit)
		case "PLpgSQL_stmt_return":
			s.Return = &plStmtReturn{}
			return json.Unmarshal(val, s.Return)
		case "PLpgSQL_stmt_return_next":
			s.ReturnNext = &plStmtReturnNext{}
			return json.Unmarshal(val, s.ReturnNext)
		case "PLpgSQL_stmt_return_query":
			s.ReturnQuery = &plStmtReturnQuery{}
			return json.Unmarshal(val, s.ReturnQuery)
		case "PLpgSQL_stmt_raise":
			s.Raise = &plStmtRaise{}
			return json.Unmarshal(val, s.Raise)
		case "PLpgSQL_stmt_execsql":
			s.ExecSQL = &plStmtExecSQL{}
			return json.Unmarshal(val, s.ExecSQL)
		case "PLpgSQL_stmt_perform":
			s.Perform = &plStmtPerform{}
			return json.Unmarshal(val, s.Perform)
		case "PLpgSQL_stmt_dynexecute":
			s.DynExecute = &plStmtDynExecute{}
			return json.Unmarshal(val, s.DynExecute)
		case "PLpgSQL_stmt_block":
			s.Block = &plStmtBlock{}
			return json.Unmarshal(val, s.Block)
		}
	}
	return nil
}
