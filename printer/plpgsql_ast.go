package printer

import (
	"encoding/json"
	"fmt"
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
	// Bound cursor declarations (name CURSOR [(args)] FOR query).
	CursorExplicitExpr *plExprW `json:"cursor_explicit_expr"`
	CursorArgRow       int      `json:"cursor_explicit_argrow"`
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
	LineNo    int        `json:"lineno"`
	Cond      plExprW    `json:"cond"`
	ThenBody  []plStmt   `json:"then_body"`
	ElsIfList []plElsIfW `json:"elsif_list"`
	ElseBody  []plStmt   `json:"else_body"`
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
	LineNo       int           `json:"lineno"`
	TExpr        *plExprW      `json:"t_expr"`
	TVarNo       int           `json:"t_varno"`
	CaseWhenList []plCaseWhenW `json:"case_when_list"`
	HaveElse     bool          `json:"have_else"`
	ElseStmts    []plStmt      `json:"else_stmts"`
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
	Label  string   `json:"label"`
	Cond   plExprW  `json:"cond"`
	Body   []plStmt `json:"body"`
}

type plStmtForI struct {
	LineNo  int      `json:"lineno"`
	Label   string   `json:"label"`
	Var     plDatum  `json:"var"`
	Lower   plExprW  `json:"lower"`
	Upper   plExprW  `json:"upper"`
	Step    *plExprW `json:"step"`
	Reverse bool     `json:"reverse"`
	Body    []plStmt `json:"body"`
}

type plStmtForS struct {
	LineNo int      `json:"lineno"`
	Label  string   `json:"label"`
	Var    plDatum  `json:"var"`
	Query  plExprW  `json:"query"`
	Body   []plStmt `json:"body"`
}

type plStmtForEachA struct {
	LineNo int      `json:"lineno"`
	Label  string   `json:"label"`
	VarNo  int      `json:"varno"`
	Slice  int      `json:"slice"`
	Expr   plExprW  `json:"expr"`
	Body   []plStmt `json:"body"`
}

// plStmtForC is a FOR loop over a bound cursor:
// FOR var IN curname [(args)] LOOP ... END LOOP;
type plStmtForC struct {
	LineNo   int      `json:"lineno"`
	Label    string   `json:"label"`
	Var      plDatum  `json:"var"`
	CurVar   int      `json:"curvar"`
	ArgQuery *plExprW `json:"argquery"`
	Body     []plStmt `json:"body"`
}

// plStmtDynForS is a FOR loop over a dynamic query:
// FOR var IN EXECUTE query [USING params] LOOP ... END LOOP;
type plStmtDynForS struct {
	LineNo int       `json:"lineno"`
	Label  string    `json:"label"`
	Var    plDatum   `json:"var"`
	Query  plExprW   `json:"query"`
	Params []plExprW `json:"params"`
	Body   []plStmt  `json:"body"`
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
	LineNo   int       `json:"lineno"`
	Query    *plExprW  `json:"query"`
	DynQuery *plExprW  `json:"dynquery"`
	Params   []plExprW `json:"params"`
}

type plStmtRaise struct {
	LineNo    int              `json:"lineno"`
	ElogLevel int              `json:"elog_level"`
	CondName  string           `json:"condname"`
	Message   string           `json:"message"`
	Params    []plExprW        `json:"params"`
	Options   []plRaiseOptionW `json:"options"`
}

type plRaiseOptionW struct {
	O plRaiseOption `json:"PLpgSQL_raise_option"`
}

type plRaiseOption struct {
	OptType int     `json:"opt_type"`
	Expr    plExprW `json:"expr"`
}

// raiseOptionName maps PLpgSQL_raise_option_type values to keywords.
var raiseOptionName = map[int]string{
	0: "ERRCODE",
	1: "MESSAGE",
	2: "DETAIL",
	3: "HINT",
	4: "COLUMN",
	5: "CONSTRAINT",
	6: "DATATYPE",
	7: "TABLE",
	8: "SCHEMA",
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

type plStmtCall struct {
	LineNo int     `json:"lineno"`
	Expr   plExprW `json:"expr"`
}

type plStmtCommit struct {
	LineNo int  `json:"lineno"`
	Chain  bool `json:"chain"`
}

type plStmtRollback struct {
	LineNo int  `json:"lineno"`
	Chain  bool `json:"chain"`
}

type plStmtGetDiag struct {
	LineNo    int           `json:"lineno"`
	IsStacked bool          `json:"is_stacked"`
	DiagItems []plDiagItemW `json:"diag_items"`
}

type plDiagItemW struct {
	I plDiagItem `json:"PLpgSQL_diag_item"`
}

type plDiagItem struct {
	Kind   string `json:"kind"`
	Target int    `json:"target"`
}

type plStmtAssert struct {
	LineNo  int      `json:"lineno"`
	Cond    plExprW  `json:"cond"`
	Message *plExprW `json:"message"`
}

type plStmtOpen struct {
	LineNo   int       `json:"lineno"`
	CurVar   int       `json:"curvar"`
	ArgQuery *plExprW  `json:"argquery"`
	Query    *plExprW  `json:"query"`
	DynQuery *plExprW  `json:"dynquery"`
	Params   []plExprW `json:"params"`
}

// Fetch directions (PostgreSQL FetchDirection enum).
const (
	plFetchForward  = 0
	plFetchBackward = 1
	plFetchAbsolute = 2
	plFetchRelative = 3
)

type plStmtFetch struct {
	LineNo    int      `json:"lineno"`
	Target    *plDatum `json:"target"`
	CurVar    int      `json:"curvar"`
	Direction int      `json:"direction"`
	HowMany   int      `json:"how_many"`
	Expr      *plExprW `json:"expr"`
	IsMove    bool     `json:"is_move"`
}

type plStmtClose struct {
	LineNo int `json:"lineno"`
	CurVar int `json:"curvar"`
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
	ForC        *plStmtForC
	DynForS     *plStmtDynForS
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
	Call        *plStmtCall
	Commit      *plStmtCommit
	Rollback    *plStmtRollback
	GetDiag     *plStmtGetDiag
	Assert      *plStmtAssert
	Open        *plStmtOpen
	Fetch       *plStmtFetch
	Close       *plStmtClose
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
	case s.ForC != nil:
		return s.ForC.LineNo
	case s.DynForS != nil:
		return s.DynForS.LineNo
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
	case s.Call != nil:
		return s.Call.LineNo
	case s.Commit != nil:
		return s.Commit.LineNo
	case s.Rollback != nil:
		return s.Rollback.LineNo
	case s.GetDiag != nil:
		return s.GetDiag.LineNo
	case s.Assert != nil:
		return s.Assert.LineNo
	case s.Open != nil:
		return s.Open.LineNo
	case s.Fetch != nil:
		return s.Fetch.LineNo
	case s.Close != nil:
		return s.Close.LineNo
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
		case "PLpgSQL_stmt_forc":
			s.ForC = &plStmtForC{}
			return json.Unmarshal(val, s.ForC)
		case "PLpgSQL_stmt_dynfors":
			s.DynForS = &plStmtDynForS{}
			return json.Unmarshal(val, s.DynForS)
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
			if err := json.Unmarshal(val, s.ReturnNext); err != nil {
				return err
			}
			if s.ReturnNext.Expr == nil {
				// RETURN NEXT <variable> is compiled to a datum reference
				// (retvarno) that libpg_query's JSON output does not include,
				// so the statement cannot be reconstructed. Fail the parse to
				// trigger the raw-body fallback rather than emitting a bare
				// RETURN NEXT; (which is invalid and drops the variable).
				return fmt.Errorf("RETURN NEXT with variable target is not representable")
			}
			return nil
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
		case "PLpgSQL_stmt_call":
			s.Call = &plStmtCall{}
			return json.Unmarshal(val, s.Call)
		case "PLpgSQL_stmt_commit":
			s.Commit = &plStmtCommit{}
			return json.Unmarshal(val, s.Commit)
		case "PLpgSQL_stmt_rollback":
			s.Rollback = &plStmtRollback{}
			return json.Unmarshal(val, s.Rollback)
		case "PLpgSQL_stmt_getdiag":
			s.GetDiag = &plStmtGetDiag{}
			return json.Unmarshal(val, s.GetDiag)
		case "PLpgSQL_stmt_assert":
			s.Assert = &plStmtAssert{}
			return json.Unmarshal(val, s.Assert)
		case "PLpgSQL_stmt_open":
			s.Open = &plStmtOpen{}
			return json.Unmarshal(val, s.Open)
		case "PLpgSQL_stmt_fetch":
			s.Fetch = &plStmtFetch{}
			return json.Unmarshal(val, s.Fetch)
		case "PLpgSQL_stmt_close":
			s.Close = &plStmtClose{}
			return json.Unmarshal(val, s.Close)
		default:
			// An unrecognized statement type MUST fail the parse: silently
			// ignoring it would drop the statement from the formatted output,
			// corrupting the function body. The error propagates up to
			// formatPLpgSQLBody, which falls back to emitting the original
			// body verbatim.
			return fmt.Errorf("unsupported PL/pgSQL statement type: %s", key)
		}
	}
	return nil
}
