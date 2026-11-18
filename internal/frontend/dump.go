package frontend

import (
	"fmt"
	"io"
	"strings"
)

// Dump writes a human-readable representation of the lowered IR.
func Dump(w io.Writer, program *Program) error {
	if program == nil {
		return fmt.Errorf("frontend: nil program")
	}
	fmt.Fprintf(w, "program locals=%v\n", program.Locals)
	return dumpBody(w, program.Body, 1)
}

func dumpBody(w io.Writer, body []Expr, depth int) error {
	for _, expr := range body {
		if err := dumpExpr(w, expr, depth); err != nil {
			return err
		}
	}
	return nil
}

func indent(depth int) string {
	return strings.Repeat("  ", depth)
}

func dumpExpr(w io.Writer, expr Expr, depth int) error {
	if expr == nil {
		fmt.Fprintf(w, "%s<nil>\n", indent(depth))
		return nil
	}
	prefix := indent(depth)
	switch e := expr.(type) {
	case *Statements:
		fmt.Fprintf(w, "%sstatements\n", prefix)
		return dumpBody(w, e.Body, depth+1)
	case *NilLiteral:
		fmt.Fprintf(w, "%snil\n", prefix)
	case *TrueLiteral:
		fmt.Fprintf(w, "%strue\n", prefix)
	case *FalseLiteral:
		fmt.Fprintf(w, "%sfalse\n", prefix)
	case *SelfExpr:
		fmt.Fprintf(w, "%sself\n", prefix)
	case *IntegerLiteral:
		fmt.Fprintf(w, "%sinteger %d\n", prefix, e.Value)
	case *FloatLiteral:
		fmt.Fprintf(w, "%sfloat %v\n", prefix, e.Value)
	case *StringLiteral:
		fmt.Fprintf(w, "%sstring %q\n", prefix, e.Value)
	case *SymbolLiteral:
		fmt.Fprintf(w, "%ssymbol :%s\n", prefix, e.Value)
	case *InterpolatedString:
		fmt.Fprintf(w, "%sinterpolated-string\n", prefix)
		for _, part := range e.Parts {
			if err := dumpExpr(w, part, depth+1); err != nil {
				return err
			}
		}
	case *LocalRead:
		fmt.Fprintf(w, "%slvar %s depth=%d\n", prefix, e.Name, e.Depth)
	case *LocalWrite:
		fmt.Fprintf(w, "%slvar-write %s depth=%d\n", prefix, e.Name, e.Depth)
		return dumpExpr(w, e.Value, depth+1)
	case *GlobalRead:
		fmt.Fprintf(w, "%sgvar %s\n", prefix, e.Name)
	case *GlobalWrite:
		fmt.Fprintf(w, "%sgvar-write %s\n", prefix, e.Name)
		return dumpExpr(w, e.Value, depth+1)
	case *InstanceRead:
		fmt.Fprintf(w, "%sivar %s\n", prefix, e.Name)
	case *InstanceWrite:
		fmt.Fprintf(w, "%sivar-write %s\n", prefix, e.Name)
		return dumpExpr(w, e.Value, depth+1)
	case *ClassVariableRead:
		fmt.Fprintf(w, "%scvar %s\n", prefix, e.Name)
	case *ClassVariableWrite:
		fmt.Fprintf(w, "%scvar-write %s\n", prefix, e.Name)
		return dumpExpr(w, e.Value, depth+1)
	case *ConstantRead:
		fmt.Fprintf(w, "%sconst %s\n", prefix, e.Name)
	case *ConstantWrite:
		fmt.Fprintf(w, "%sconst-write %s\n", prefix, e.Name)
		return dumpExpr(w, e.Value, depth+1)
	case *ConstantPathRead:
		fmt.Fprintf(w, "%sconst-path %s\n", prefix, e.Name)
		return dumpExpr(w, e.Parent, depth+1)
	case *ClassDef:
		fmt.Fprintf(w, "%sclass %s locals=%v\n", prefix, e.Name, e.Locals)
		if e.Outer != nil {
			fmt.Fprintf(w, "%s  outer\n", prefix)
			if err := dumpExpr(w, e.Outer, depth+2); err != nil {
				return err
			}
		}
		if e.Superclass != nil {
			fmt.Fprintf(w, "%s  superclass\n", prefix)
			if err := dumpExpr(w, e.Superclass, depth+2); err != nil {
				return err
			}
		}
		return dumpBody(w, e.Body, depth+1)
	case *ModuleDef:
		fmt.Fprintf(w, "%smodule %s locals=%v\n", prefix, e.Name, e.Locals)
		if e.Outer != nil {
			fmt.Fprintf(w, "%s  outer\n", prefix)
			if err := dumpExpr(w, e.Outer, depth+2); err != nil {
				return err
			}
		}
		return dumpBody(w, e.Body, depth+1)
	case *SingletonClassDef:
		fmt.Fprintf(w, "%ssingleton-class locals=%v\n", prefix, e.Locals)
		if err := dumpExpr(w, e.Expression, depth+1); err != nil {
			return err
		}
		return dumpBody(w, e.Body, depth+1)
	case *ForExpr:
		fmt.Fprintf(w, "%sfor %s\n", prefix, e.Target.Name)
		fmt.Fprintf(w, "%s  collection\n", prefix)
		if err := dumpExpr(w, e.Collection, depth+2); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s  body\n", prefix)
		return dumpBody(w, e.Body, depth+2)
	case *BeginExpr:
		fmt.Fprintf(w, "%sbegin\n", prefix)
		if err := dumpBody(w, e.Body, depth+1); err != nil {
			return err
		}
		for _, rescue := range e.Rescues {
			fmt.Fprintf(w, "%srescue\n", prefix)
			for _, classExpr := range rescue.Classes {
				if err := dumpExpr(w, classExpr, depth+1); err != nil {
					return err
				}
			}
			if err := dumpBody(w, rescue.Body, depth+1); err != nil {
				return err
			}
		}
		if len(e.Else) > 0 {
			fmt.Fprintf(w, "%selse\n", prefix)
			if err := dumpBody(w, e.Else, depth+1); err != nil {
				return err
			}
		}
		if len(e.Ensure) > 0 {
			fmt.Fprintf(w, "%sensure\n", prefix)
			return dumpBody(w, e.Ensure, depth+1)
		}
	case *RetryExpr:
		fmt.Fprintf(w, "%sretry\n", prefix)
	case *RedoExpr:
		fmt.Fprintf(w, "%sredo\n", prefix)
	case *AliasExpr:
		fmt.Fprintf(w, "%salias %s %s\n", prefix, e.NewName, e.OldName)
	case *UndefExpr:
		fmt.Fprintf(w, "%sundef %v\n", prefix, e.Names)
	case *LambdaExpr:
		fmt.Fprintf(w, "%slambda params=%v locals=%v\n", prefix, paramNames(e.Params), e.Locals)
		return dumpBody(w, e.Body, depth+1)
	case *SuperExpr:
		fmt.Fprintf(w, "%ssuper forwarding=%v nargs=%d\n", prefix, e.Forwarding, len(e.Args))
		for _, arg := range e.Args {
			if err := dumpExpr(w, arg, depth+1); err != nil {
				return err
			}
		}
		if e.BlockArg != nil {
			fmt.Fprintf(w, "%s  block-arg\n", prefix)
			if err := dumpExpr(w, e.BlockArg, depth+2); err != nil {
				return err
			}
		}
	case *MultiWrite:
		fmt.Fprintf(w, "%smultiple-assignment targets=%d\n", prefix, len(e.Targets))
		return dumpExpr(w, e.Value, depth+1)
	case *SplatArg:
		fmt.Fprintf(w, "%ssplat\n", prefix)
		return dumpExpr(w, e.Value, depth+1)
	case *ArrayLiteral:
		fmt.Fprintf(w, "%sarray\n", prefix)
		for _, element := range e.Elements {
			if err := dumpExpr(w, element, depth+1); err != nil {
				return err
			}
		}
	case *HashLiteral:
		fmt.Fprintf(w, "%shash\n", prefix)
		for _, pair := range e.Pairs {
			fmt.Fprintf(w, "%s  key\n", prefix)
			if err := dumpExpr(w, pair.Key, depth+2); err != nil {
				return err
			}
			fmt.Fprintf(w, "%s  value\n", prefix)
			if err := dumpExpr(w, pair.Value, depth+2); err != nil {
				return err
			}
		}
	case *RangeLiteral:
		exclusive := ".."
		if e.Exclusive {
			exclusive = "..."
		}
		fmt.Fprintf(w, "%srange %s\n", prefix, exclusive)
		if err := dumpExpr(w, e.Low, depth+1); err != nil {
			return err
		}
		return dumpExpr(w, e.High, depth+1)
	case *Call:
		receiver := "self"
		if e.Receiver != nil {
			receiver = "explicit"
		}
		fmt.Fprintf(w, "%scall %s (%s) nargs=%d\n", prefix, e.Name, receiver, len(e.Args))
		if e.Receiver != nil {
			if err := dumpExpr(w, e.Receiver, depth+1); err != nil {
				return err
			}
		}
		for _, arg := range e.Args {
			if err := dumpExpr(w, arg, depth+1); err != nil {
				return err
			}
		}
		for _, pair := range e.Keywords {
			fmt.Fprintf(w, "%s  keyword\n", prefix)
			if err := dumpExpr(w, pair.Key, depth+2); err != nil {
				return err
			}
			if err := dumpExpr(w, pair.Value, depth+2); err != nil {
				return err
			}
		}
		if e.KeywordSplat != nil {
			fmt.Fprintf(w, "%s  keyword-splat\n", prefix)
			if err := dumpExpr(w, e.KeywordSplat, depth+2); err != nil {
				return err
			}
		}
		if e.BlockArg != nil {
			fmt.Fprintf(w, "%s  block-arg\n", prefix)
			if err := dumpExpr(w, e.BlockArg, depth+2); err != nil {
				return err
			}
		}
		if e.Block != nil {
			fmt.Fprintf(w, "%s  block params=%v locals=%v\n", prefix, paramNames(e.Block.Params), e.Block.Locals)
			if err := dumpBody(w, e.Block.Body, depth+2); err != nil {
				return err
			}
		}
	case *IfExpr:
		fmt.Fprintf(w, "%sif\n", prefix)
		fmt.Fprintf(w, "%s  cond\n", prefix)
		if err := dumpExpr(w, e.Cond, depth+2); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s  then\n", prefix)
		if err := dumpExpr(w, e.Then, depth+2); err != nil {
			return err
		}
		if e.Else != nil {
			fmt.Fprintf(w, "%s  else\n", prefix)
			return dumpExpr(w, e.Else, depth+2)
		}
	case *WhileExpr:
		fmt.Fprintf(w, "%swhile\n", prefix)
		fmt.Fprintf(w, "%s  cond\n", prefix)
		if err := dumpExpr(w, e.Cond, depth+2); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s  body\n", prefix)
		return dumpExpr(w, e.Body, depth+2)
	case *CaseExpr:
		fmt.Fprintf(w, "%scase\n", prefix)
		if e.Subject != nil {
			fmt.Fprintf(w, "%s  subject\n", prefix)
			if err := dumpExpr(w, e.Subject, depth+2); err != nil {
				return err
			}
		}
		for _, when := range e.Whens {
			fmt.Fprintf(w, "%s  when\n", prefix)
			for _, cond := range when.Conds {
				if err := dumpExpr(w, cond, depth+2); err != nil {
					return err
				}
			}
			fmt.Fprintf(w, "%s  then\n", prefix)
			if err := dumpBody(w, when.Body, depth+2); err != nil {
				return err
			}
		}
		if e.Else != nil {
			fmt.Fprintf(w, "%s  else\n", prefix)
			return dumpExpr(w, e.Else, depth+2)
		}
	case *DefExpr:
		fmt.Fprintf(w, "%sdef %s params=%v locals=%v\n", prefix, e.Name, paramNames(e.Params), e.Locals)
		return dumpBody(w, e.Body, depth+1)
	case *ReturnExpr:
		fmt.Fprintf(w, "%sreturn\n", prefix)
		if e.Value != nil {
			return dumpExpr(w, e.Value, depth+1)
		}
	case *BreakExpr:
		fmt.Fprintf(w, "%sbreak\n", prefix)
		if e.Value != nil {
			return dumpExpr(w, e.Value, depth+1)
		}
	case *NextExpr:
		fmt.Fprintf(w, "%snext\n", prefix)
		if e.Value != nil {
			return dumpExpr(w, e.Value, depth+1)
		}
	case *YieldExpr:
		fmt.Fprintf(w, "%syield nargs=%d\n", prefix, len(e.Args))
		for _, arg := range e.Args {
			if err := dumpExpr(w, arg, depth+1); err != nil {
				return err
			}
		}
	case *AndExpr:
		fmt.Fprintf(w, "%sand\n", prefix)
		if err := dumpExpr(w, e.Left, depth+1); err != nil {
			return err
		}
		return dumpExpr(w, e.Right, depth+1)
	case *OrExpr:
		fmt.Fprintf(w, "%sor\n", prefix)
		if err := dumpExpr(w, e.Left, depth+1); err != nil {
			return err
		}
		return dumpExpr(w, e.Right, depth+1)
	case *NotExpr:
		fmt.Fprintf(w, "%snot\n", prefix)
		return dumpExpr(w, e.Value, depth+1)
	default:
		fmt.Fprintf(w, "%sunknown %T\n", prefix, expr)
	}
	return nil
}

func paramNames(params []Param) []string {
	names := make([]string, len(params))
	for i, param := range params {
		prefix := ""
		suffix := ""
		switch param.Kind {
		case ParamOptional:
			prefix = "?"
		case ParamRest:
			prefix = "*"
		case ParamBlock:
			prefix = "&"
		case ParamKeyword:
			suffix = ":"
		case ParamOptionalKeyword:
			prefix = "?"
			suffix = ":"
		case ParamKeywordRest:
			prefix = "**"
		}
		names[i] = prefix + param.Name + suffix
	}
	return names
}
