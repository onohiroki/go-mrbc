package frontend

// Program is the lowered form of a top-level Ruby program.
type Program struct {
	Body   []Expr
	Locals []string
	Pos    Position
}

// Expr is a lowered expression.
type Expr interface {
	exprNode()
	Position() Position
}

type exprBase struct {
	Pos Position
}

func (e exprBase) exprNode()          {}
func (e exprBase) Position() Position { return e.Pos }

// Statements evaluates its body in order and yields the last value, or nil.
type Statements struct {
	exprBase
	Body []Expr
}

type NilLiteral struct{ exprBase }

type TrueLiteral struct{ exprBase }

type FalseLiteral struct{ exprBase }

type SelfExpr struct{ exprBase }

type IntegerLiteral struct {
	exprBase
	Value int64
}

type FloatLiteral struct {
	exprBase
	Value float64
}

type StringLiteral struct {
	exprBase
	Value string
}

type SymbolLiteral struct {
	exprBase
	Value string
}

type ArrayLiteral struct {
	exprBase
	Elements []Expr
}

type HashPair struct {
	Key   Expr
	Value Expr
}

type HashLiteral struct {
	exprBase
	Pairs []HashPair
}

type RangeLiteral struct {
	exprBase
	Low       Expr
	High      Expr
	Exclusive bool
}

// InterpolatedString is a string built from literal parts and expressions.
type InterpolatedString struct {
	exprBase
	Parts []Expr
}

type LocalRead struct {
	exprBase
	Name  string
	Depth int
}

type LocalWrite struct {
	exprBase
	Name  string
	Depth int
	Value Expr
}

type GlobalRead struct {
	exprBase
	Name string
}

type GlobalWrite struct {
	exprBase
	Name  string
	Value Expr
}

type ConstantRead struct {
	exprBase
	Name string
}

// Call is a method call. A nil Receiver means an implicit-self call.
type Call struct {
	exprBase
	Receiver Expr
	Name     string
	Args     []Expr
	Block    *Block
}

func (c *Call) ImplicitSelf() bool { return c.Receiver == nil }

type Block struct {
	Pos    Position
	Params []Param
	Body   []Expr
	Locals []string
}

type ParamKind uint8

const (
	ParamRequired ParamKind = iota
	ParamOptional
	ParamRest
	ParamBlock
)

type Param struct {
	Pos     Position
	Name    string
	Kind    ParamKind
	Default Expr
}

type IfExpr struct {
	exprBase
	Cond Expr
	Then Expr
	Else Expr // may be nil
}

type WhileExpr struct {
	exprBase
	Cond Expr
	Body Expr
}

type CaseExpr struct {
	exprBase
	Subject Expr // may be nil
	Whens   []When
	Else    Expr // may be nil
}

type When struct {
	Pos   Position
	Conds []Expr
	Body  []Expr
}

type DefExpr struct {
	exprBase
	Name     string
	Receiver Expr // nil for ordinary methods
	Params   []Param
	Body     []Expr
	Locals   []string
}

type ReturnExpr struct {
	exprBase
	Value Expr // may be nil
}

type BreakExpr struct {
	exprBase
	Value Expr // may be nil
}

type NextExpr struct {
	exprBase
	Value Expr // may be nil
}

type YieldExpr struct {
	exprBase
	Args []Expr
}

type AndExpr struct {
	exprBase
	Left  Expr
	Right Expr
}

type OrExpr struct {
	exprBase
	Left  Expr
	Right Expr
}

type NotExpr struct {
	exprBase
	Value Expr
}

func base(pos Position) exprBase { return exprBase{Pos: pos} }
