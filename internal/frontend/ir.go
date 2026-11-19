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

// ObjectClassExpr compiles to OCLASS. It denotes the class of the current
// self and is used as the outer scope of an absolute constant path such as
// `class ::C`.
type ObjectClassExpr struct{ exprBase }

type IntegerLiteral struct {
	exprBase
	Value int64
}

// BigIntLiteral is an integer literal that does not fit int64. Digits holds
// the decimal magnitude without a sign.
type BigIntLiteral struct {
	exprBase
	Digits   string
	Negative bool
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

type SymbolInterpolation struct {
	exprBase
	Parts []Expr
}

type RegexLiteral struct {
	exprBase
	Source string
	Flags  string
}

type RegexInterpolation struct {
	exprBase
	Parts []Expr
	Flags string
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

// KeywordArg is one keyword argument. A nil Key denotes a keyword splat;
// Value then holds the hash expression.
type KeywordArg struct {
	Key   Expr
	Value Expr
}

func (k KeywordArg) IsSplat() bool { return k.Key == nil }

// Call is a method call. A nil Receiver means an implicit-self call.
type Call struct {
	exprBase
	Receiver       Expr
	Name           string
	Args           []Expr // may contain *SplatArg
	Keywords       []KeywordArg
	ForwardAll     bool
	SafeNavigation bool
	Block          *Block
	BlockArg       Expr
}

func (c *Call) ImplicitSelf() bool { return c.Receiver == nil }

// SplatArg is a splatted positional argument in a call.
type SplatArg struct {
	exprBase
	Value Expr
}

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
	ParamPost
	ParamBlock
	ParamKeyword
	ParamOptionalKeyword
	ParamKeywordRest
	ParamDestructure
	ParamNoBlock
)

type Param struct {
	Pos         Position
	Name        string
	Kind        ParamKind
	Default     Expr
	Destructure *MultiTarget
}

type IfExpr struct {
	exprBase
	Cond Expr
	Then Expr
	Else Expr // may be nil
}

type WhileExpr struct {
	exprBase
	Cond    Expr
	Body    Expr
	DoWhile bool
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
	Args     []Expr
	Keywords []KeywordArg
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

type InstanceRead struct {
	exprBase
	Name string
}

type InstanceWrite struct {
	exprBase
	Name  string
	Value Expr
}

type ClassVariableRead struct {
	exprBase
	Name string
}

type ClassVariableWrite struct {
	exprBase
	Name  string
	Value Expr
}

type ConstantWrite struct {
	exprBase
	Name  string
	Value Expr
}

type ConstantPathRead struct {
	exprBase
	Parent Expr
	Name   string
}

type ConstantPathWrite struct {
	exprBase
	Parent Expr // nil for ::Name
	Name   string
	Value  Expr
}

type ClassDef struct {
	exprBase
	Name       string
	Outer      Expr // nil for a top-level class name
	Superclass Expr // nil for Object
	Body       []Expr
	Locals     []string
}

type ModuleDef struct {
	exprBase
	Name   string
	Outer  Expr
	Body   []Expr
	Locals []string
}

type SingletonClassDef struct {
	exprBase
	Expression Expr
	Body       []Expr
	Locals     []string
}

type SuperExpr struct {
	exprBase
	Args       []Expr
	Keywords   []KeywordArg
	Forwarding bool
	Block      *Block
	BlockArg   Expr
}

type LambdaExpr struct {
	exprBase
	Params []Param
	Body   []Expr
	Locals []string
}

type MultiTargetKind uint8

const (
	TargetLocal MultiTargetKind = iota
	TargetGlobal
	TargetInstance
	TargetConstant
	TargetIndex
	TargetCall
	TargetNested
)

type MultiTarget struct {
	Pos      Position
	Kind     MultiTargetKind
	Name     string
	Depth    int
	Receiver Expr
	Args     []Expr

	Lefts []MultiTarget
	Rest  *MultiTarget
	Posts []MultiTarget
}

type MultiWrite struct {
	exprBase
	Targets []MultiTarget
	Rest    *MultiTarget
	Posts   []MultiTarget
	Value   Expr
}

type ForExpr struct {
	exprBase
	Targets    []MultiTarget
	Rest       *MultiTarget
	Posts      []MultiTarget
	Collection Expr
	Body       []Expr
}

type BeginExpr struct {
	exprBase
	Body    []Expr
	Rescues []RescueClause
	Else    []Expr
	Ensure  []Expr
}

type RescueClause struct {
	Pos     Position
	Classes []Expr
	Target  *MultiTarget
	Body    []Expr
}

type RetryExpr struct{ exprBase }

type RedoExpr struct{ exprBase }

type AliasExpr struct {
	exprBase
	NewName string
	OldName string
}

type MatchWriteExpr struct {
	exprBase
	Call    Expr
	Targets []MultiTarget
}

type UndefExpr struct {
	exprBase
	Names []string
}

// BlockSlotExpr reads the enclosing method's implicit block argument.
type BlockSlotExpr struct {
	exprBase
}

type CaseMatchExpr struct {
	exprBase
	Subject Expr
	Ins     []InClause
	Else    []Expr
	HasElse bool
}

type InClause struct {
	Pos     Position
	Pattern Pattern
	Guard   Expr
	Body    []Expr
}

type Pattern interface {
	patternNode()
	Position() Position
}

type patternBase struct {
	Pos Position
}

func (p patternBase) patternNode()       {}
func (p patternBase) Position() Position { return p.Pos }

type ValuePattern struct {
	patternBase
	Value Expr
}

type EqualPattern struct {
	patternBase
	Value Expr
}

type BindPattern struct {
	patternBase
	Target MultiTarget
}

type CapturePattern struct {
	patternBase
	Value  Pattern
	Target MultiTarget
}

type ArrayPattern struct {
	patternBase
	Constant  Expr
	Requireds []Pattern
	Rest      *Pattern
	HasRest   bool
	Posts     []Pattern
}

type FindPattern struct {
	patternBase
	Constant  Expr
	Left      *Pattern
	Requireds []Pattern
	Right     *Pattern
}

type AlternationPattern struct {
	patternBase
	Left  Pattern
	Right Pattern
}

type MatchRequiredExpr struct {
	exprBase
	Value   Expr
	Pattern Pattern
}

type MatchPredicateExpr struct {
	exprBase
	Value   Expr
	Pattern Pattern
}

type HashPatternEntry struct {
	Key   string
	Value Pattern
}

type HashPattern struct {
	patternBase
	Constant Expr
	Entries  []HashPatternEntry
	Rest     *MultiTarget
}

func pattern(pos Position) patternBase { return patternBase{Pos: pos} }

func base(pos Position) exprBase { return exprBase{Pos: pos} }
