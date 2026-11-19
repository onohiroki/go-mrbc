package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/danielgatis/go-ruby-prism/parser"
)

// Lower parses src with Prism and lowers it to the internal IR.
func Lower(ctx context.Context, src []byte, filename string) (*Program, error) {
	p, err := parser.NewParser(ctx,
		parser.WithFilePath(filename),
		parser.WithMainScript(true),
		parser.WithEncoding("UTF-8"),
	)
	if err != nil {
		return nil, fmt.Errorf("create parser: %w", err)
	}
	defer p.Close(ctx)
	return LowerWithParser(ctx, p, src, filename)
}

// LowerWithParser parses src using an existing parser.
func LowerWithParser(ctx context.Context, p *parser.Parser, src []byte, filename string) (*Program, error) {
	result, err := p.Parse(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	source := NewSource(filename, src)
	if len(result.Errors) > 0 {
		first := result.Errors[0]
		pos := source.Position(first.Location.StartOffset)
		return nil, &Error{
			File:     filename,
			Position: pos,
			Message:  first.Message,
		}
	}
	if result.Value == nil {
		return nil, &Error{File: filename, Position: Position{Line: 1, Column: 1}, Message: "empty parse result"}
	}

	l := &lowerer{source: source}
	body, err := l.lowerStatements(result.Value.Statements)
	if err != nil {
		return nil, err
	}
	return &Program{
		Body:   body,
		Locals: append([]string(nil), result.Value.Locals...),
		Pos:    l.pos(result.Value),
	}, nil
}

// DumpAST returns the Prism AST encoded as JSON.
func DumpAST(ctx context.Context, src []byte, filename string) ([]byte, error) {
	p, err := parser.NewParser(ctx,
		parser.WithFilePath(filename),
		parser.WithMainScript(true),
		parser.WithEncoding("UTF-8"),
	)
	if err != nil {
		return nil, fmt.Errorf("create parser: %w", err)
	}
	defer p.Close(ctx)
	result, err := p.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	source := NewSource(filename, src)
	if len(result.Errors) > 0 {
		first := result.Errors[0]
		return nil, &Error{
			File:     filename,
			Position: source.Position(first.Location.StartOffset),
			Message:  first.Message,
		}
	}
	return json.MarshalIndent(result, "", "  ")
}

type lowerer struct {
	source *Source
}

func (l *lowerer) pos(node parser.Node) Position {
	if node == nil {
		return Position{Line: 1, Column: 1}
	}
	return l.source.Position(node.GetLocation().StartOffset)
}

func (l *lowerer) errorf(node parser.Node, format string, args ...any) error {
	offset := 0
	if node != nil {
		offset = node.GetLocation().StartOffset
	}
	return l.source.errorf(offset, format, args...)
}

func (l *lowerer) unsupported(node parser.Node) error {
	return l.errorf(node, "unsupported syntax: %s", nodeName(node))
}

func nodeName(node parser.Node) string {
	if node == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(node)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func (l *lowerer) lowerStatements(statements *parser.StatementsNode) ([]Expr, error) {
	if statements == nil {
		return nil, nil
	}
	out := make([]Expr, 0, len(statements.Body))
	for _, node := range statements.Body {
		expr, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		out = append(out, expr)
	}
	return out, nil
}

func (l *lowerer) lowerBody(node parser.Node) ([]Expr, error) {
	if node == nil {
		return nil, nil
	}
	switch n := node.(type) {
	case *parser.StatementsNode:
		return l.lowerStatements(n)
	case *parser.BeginNode:
		expr, err := l.lowerBegin(n)
		if err != nil {
			return nil, err
		}
		return []Expr{expr}, nil
	default:
		expr, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		return []Expr{expr}, nil
	}
}

func (l *lowerer) lowerExpr(node parser.Node) (Expr, error) {
	if node == nil {
		return nil, l.source.errorf(0, "missing expression")
	}
	pos := l.pos(node)

	switch n := node.(type) {
	case *parser.StatementsNode:
		body, err := l.lowerStatements(n)
		if err != nil {
			return nil, err
		}
		return &Statements{exprBase: base(pos), Body: body}, nil

	case *parser.NilNode:
		return &NilLiteral{exprBase: base(pos)}, nil
	case *parser.TrueNode:
		return &TrueLiteral{exprBase: base(pos)}, nil
	case *parser.FalseNode:
		return &FalseLiteral{exprBase: base(pos)}, nil
	case *parser.SelfNode:
		return &SelfExpr{exprBase: base(pos)}, nil
	case *parser.IntegerNode:
		return l.lowerInteger(n, pos)
	case *parser.FloatNode:
		return &FloatLiteral{exprBase: base(pos), Value: n.Value}, nil
	case *parser.StringNode:
		return &StringLiteral{exprBase: base(pos), Value: n.Unescaped.Value}, nil
	case *parser.SymbolNode:
		return &SymbolLiteral{exprBase: base(pos), Value: n.Unescaped.Value}, nil
	case *parser.ImplicitNode:
		return l.lowerExpr(n.Value)

	case *parser.InterpolatedStringNode:
		return l.lowerInterpolatedString(n)
	case *parser.XStringNode:
		return &Call{
			exprBase: base(pos),
			Name:     "`",
			Args:     []Expr{&StringLiteral{exprBase: base(pos), Value: n.Unescaped.Value}},
		}, nil
	case *parser.InterpolatedXStringNode:
		parts, err := l.lowerStringParts(n.Parts)
		if err != nil {
			return nil, err
		}
		return &Call{
			exprBase: base(pos),
			Name:     "`",
			Args:     []Expr{&InterpolatedString{exprBase: base(pos), Parts: parts}},
		}, nil
	case *parser.InterpolatedSymbolNode:
		parts, err := l.lowerStringParts(n.Parts)
		if err != nil {
			return nil, err
		}
		return &SymbolInterpolation{exprBase: base(pos), Parts: parts}, nil
	case *parser.RegularExpressionNode:
		return &RegexLiteral{
			exprBase: base(pos),
			Source:   n.Unescaped.Value,
			Flags:    regexFlags(n),
		}, nil
	case *parser.InterpolatedRegularExpressionNode:
		parts, err := l.lowerStringParts(n.Parts)
		if err != nil {
			return nil, err
		}
		return &RegexInterpolation{exprBase: base(pos), Parts: parts, Flags: regexFlags(n)}, nil
	case *parser.RationalNode:
		args := []Expr{&IntegerLiteral{exprBase: base(pos), Value: n.Numerator}}
		if n.Denominator != 1 {
			args = append(args, &IntegerLiteral{exprBase: base(pos), Value: n.Denominator})
		}
		return &Call{exprBase: base(pos), Name: "Rational", Args: args}, nil
	case *parser.ImaginaryNode:
		numeric, err := l.lowerExpr(n.Numeric)
		if err != nil {
			return nil, err
		}
		return &Call{
			exprBase: base(pos),
			Name:     "Complex",
			Args: []Expr{
				&IntegerLiteral{exprBase: base(pos), Value: 0},
				numeric,
			},
		}, nil

	case *parser.LocalVariableReadNode:
		return &LocalRead{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth)}, nil
	case *parser.LocalVariableWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &LocalWrite{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth), Value: value}, nil
	case *parser.LocalVariableOperatorWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &LocalRead{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth)}
		call := &Call{
			exprBase: base(pos),
			Receiver: read,
			Name:     n.BinaryOperator,
			Args:     []Expr{value},
		}
		return &LocalWrite{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth), Value: call}, nil
	case *parser.LocalVariableOrWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &OrExpr{
			exprBase: base(pos),
			Left:     &LocalRead{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth)},
			Right:    &LocalWrite{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth), Value: value},
		}, nil
	case *parser.LocalVariableAndWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &AndExpr{
			exprBase: base(pos),
			Left:     &LocalRead{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth)},
			Right:    &LocalWrite{exprBase: base(pos), Name: n.Name, Depth: int(n.Depth), Value: value},
		}, nil

	case *parser.GlobalVariableReadNode:
		return &GlobalRead{exprBase: base(pos), Name: n.Name}, nil
	case *parser.GlobalVariableWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &GlobalWrite{exprBase: base(pos), Name: n.Name, Value: value}, nil
	case *parser.GlobalVariableOperatorWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		call := &Call{
			exprBase: base(pos),
			Receiver: &GlobalRead{exprBase: base(pos), Name: n.Name},
			Name:     n.BinaryOperator,
			Args:     []Expr{value},
		}
		return &GlobalWrite{exprBase: base(pos), Name: n.Name, Value: call}, nil
	case *parser.GlobalVariableOrWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &OrExpr{
			exprBase: base(pos),
			Left:     &GlobalRead{exprBase: base(pos), Name: n.Name},
			Right:    &GlobalWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil
	case *parser.GlobalVariableAndWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &AndExpr{
			exprBase: base(pos),
			Left:     &GlobalRead{exprBase: base(pos), Name: n.Name},
			Right:    &GlobalWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil

	case *parser.ConstantReadNode:
		return &ConstantRead{exprBase: base(pos), Name: n.Name}, nil
	case *parser.ConstantPathNode:
		if n.Parent == nil {
			if n.Name == nil {
				return nil, l.errorf(n, "dynamic constant name is not supported")
			}
			return &ConstantRead{exprBase: base(pos), Name: *n.Name}, nil
		}
		parent, err := l.lowerExpr(n.Parent)
		if err != nil {
			return nil, err
		}
		if n.Name == nil {
			return nil, l.errorf(n, "dynamic constant name is not supported")
		}
		return &ConstantPathRead{exprBase: base(pos), Parent: parent, Name: *n.Name}, nil
	case *parser.ConstantWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &ConstantWrite{exprBase: base(pos), Name: n.Name, Value: value}, nil
	case *parser.ConstantOperatorWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &ConstantRead{exprBase: base(pos), Name: n.Name}
		return &ConstantWrite{
			exprBase: base(pos),
			Name:     n.Name,
			Value:    l.binaryAssign(read, n.BinaryOperator, value, pos, func(v Expr) Expr { return v }),
		}, nil
	case *parser.ConstantAndWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := softConstantRead(&ConstantRead{exprBase: base(pos), Name: n.Name}, pos)
		return &AndExpr{
			exprBase: base(pos),
			Left:     read,
			Right:    &ConstantWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil
	case *parser.ConstantOrWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := softConstantRead(&ConstantRead{exprBase: base(pos), Name: n.Name}, pos)
		return &OrExpr{
			exprBase: base(pos),
			Left:     read,
			Right:    &ConstantWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil

	case *parser.InstanceVariableReadNode:
		return &InstanceRead{exprBase: base(pos), Name: n.Name}, nil
	case *parser.InstanceVariableWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &InstanceWrite{exprBase: base(pos), Name: n.Name, Value: value}, nil
	case *parser.InstanceVariableOperatorWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		call := &Call{
			exprBase: base(pos),
			Receiver: &InstanceRead{exprBase: base(pos), Name: n.Name},
			Name:     n.BinaryOperator,
			Args:     []Expr{value},
		}
		return &InstanceWrite{exprBase: base(pos), Name: n.Name, Value: call}, nil
	case *parser.InstanceVariableOrWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &OrExpr{
			exprBase: base(pos),
			Left:     &InstanceRead{exprBase: base(pos), Name: n.Name},
			Right:    &InstanceWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil
	case *parser.InstanceVariableAndWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &AndExpr{
			exprBase: base(pos),
			Left:     &InstanceRead{exprBase: base(pos), Name: n.Name},
			Right:    &InstanceWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil
	case *parser.ClassVariableReadNode:
		return &ClassVariableRead{exprBase: base(pos), Name: n.Name}, nil
	case *parser.ClassVariableWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &ClassVariableWrite{exprBase: base(pos), Name: n.Name, Value: value}, nil
	case *parser.ClassVariableOperatorWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		call := &Call{
			exprBase: base(pos),
			Receiver: &ClassVariableRead{exprBase: base(pos), Name: n.Name},
			Name:     n.BinaryOperator,
			Args:     []Expr{value},
		}
		return &ClassVariableWrite{exprBase: base(pos), Name: n.Name, Value: call}, nil
	case *parser.ClassVariableOrWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &OrExpr{
			exprBase: base(pos),
			Left:     &ClassVariableRead{exprBase: base(pos), Name: n.Name},
			Right:    &ClassVariableWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil
	case *parser.ClassVariableAndWriteNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &AndExpr{
			exprBase: base(pos),
			Left:     &ClassVariableRead{exprBase: base(pos), Name: n.Name},
			Right:    &ClassVariableWrite{exprBase: base(pos), Name: n.Name, Value: value},
		}, nil

	case *parser.ArrayNode:
		elements := make([]Expr, 0, len(n.Elements))
		for _, element := range n.Elements {
			if splat, ok := element.(*parser.SplatNode); ok {
				value, err := l.lowerExpr(splat.Expression)
				if err != nil {
					return nil, err
				}
				elements = append(elements, &SplatArg{exprBase: base(l.pos(element)), Value: value})
				continue
			}
			expr, err := l.lowerExpr(element)
			if err != nil {
				return nil, err
			}
			elements = append(elements, expr)
		}
		return &ArrayLiteral{exprBase: base(pos), Elements: elements}, nil

	case *parser.HashNode:
		pairs := make([]HashPair, 0, len(n.Elements))
		for _, element := range n.Elements {
			switch entry := element.(type) {
			case *parser.AssocNode:
				key, err := l.lowerExpr(entry.Key)
				if err != nil {
					return nil, err
				}
				value, err := l.lowerExpr(entry.Value)
				if err != nil {
					return nil, err
				}
				pairs = append(pairs, HashPair{Key: key, Value: value})
			case *parser.AssocSplatNode:
				value, err := l.lowerExpr(entry.Value)
				if err != nil {
					return nil, err
				}
				pairs = append(pairs, HashPair{Value: value})
			default:
				return nil, l.unsupported(element)
			}
		}
		return &HashLiteral{exprBase: base(pos), Pairs: pairs}, nil

	case *parser.RangeNode:
		var low, high Expr
		var err error
		if n.Left != nil {
			low, err = l.lowerExpr(n.Left)
			if err != nil {
				return nil, err
			}
		}
		if n.Right != nil {
			high, err = l.lowerExpr(n.Right)
			if err != nil {
				return nil, err
			}
		}
		return &RangeLiteral{exprBase: base(pos), Low: low, High: high, Exclusive: n.IsEXCLUDE_END()}, nil

	case *parser.ParenthesesNode:
		body, err := l.lowerBody(n.Body)
		if err != nil {
			return nil, err
		}
		return statementsExpr(body, pos), nil

	case *parser.IfNode:
		return l.lowerIf(n)
	case *parser.UnlessNode:
		cond, err := l.lowerExpr(n.Predicate)
		if err != nil {
			return nil, err
		}
		negated := &NotExpr{exprBase: base(pos), Value: cond}
		thenBody, err := l.lowerStatements(n.Statements)
		if err != nil {
			return nil, err
		}
		var elseExpr Expr
		if n.ElseClause != nil {
			elseBody, err := l.lowerStatements(n.ElseClause.Statements)
			if err != nil {
				return nil, err
			}
			elseExpr = statementsExpr(elseBody, l.pos(n.ElseClause))
		}
		return &IfExpr{
			exprBase: base(pos),
			Cond:     negated,
			Then:     statementsExpr(thenBody, pos),
			Else:     elseExpr,
		}, nil

	case *parser.WhileNode:
		cond, err := l.lowerExpr(n.Predicate)
		if err != nil {
			return nil, err
		}
		body, err := l.lowerStatements(n.Statements)
		if err != nil {
			return nil, err
		}
		return &WhileExpr{
			exprBase: base(pos),
			Cond:     cond,
			Body:     statementsExpr(body, pos),
			DoWhile:  n.IsBEGIN_MODIFIER(),
		}, nil

	case *parser.UntilNode:
		cond, err := l.lowerExpr(n.Predicate)
		if err != nil {
			return nil, err
		}
		body, err := l.lowerStatements(n.Statements)
		if err != nil {
			return nil, err
		}
		return &WhileExpr{
			exprBase: base(pos),
			Cond:     &NotExpr{exprBase: base(pos), Value: cond},
			Body:     statementsExpr(body, pos),
			DoWhile:  n.IsBEGIN_MODIFIER(),
		}, nil

	case *parser.CaseNode:
		return l.lowerCase(n)
	case *parser.CaseMatchNode:
		return l.lowerCaseMatch(n)
	case *parser.ForNode:
		return l.lowerFor(n)
	case *parser.DefNode:
		return l.lowerDef(n)

	case *parser.ReturnNode:
		value, err := l.lowerMultiValue(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &ReturnExpr{exprBase: base(pos), Value: value}, nil
	case *parser.BreakNode:
		value, err := l.lowerMultiValue(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &BreakExpr{exprBase: base(pos), Value: value}, nil
	case *parser.NextNode:
		value, err := l.lowerMultiValue(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &NextExpr{exprBase: base(pos), Value: value}, nil

	case *parser.YieldNode:
		args, keywords, blockArg, forwardAll, err := l.lowerCallParts(n.Arguments)
		if err != nil {
			return nil, err
		}
		if blockArg != nil || forwardAll {
			return nil, l.errorf(n, "block arguments to yield are not supported")
		}
		return &YieldExpr{exprBase: base(pos), Args: args, Keywords: keywords}, nil

	case *parser.AndNode:
		left, err := l.lowerExpr(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := l.lowerExpr(n.Right)
		if err != nil {
			return nil, err
		}
		return &AndExpr{exprBase: base(pos), Left: left, Right: right}, nil
	case *parser.OrNode:
		left, err := l.lowerExpr(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := l.lowerExpr(n.Right)
		if err != nil {
			return nil, err
		}
		return &OrExpr{exprBase: base(pos), Left: left, Right: right}, nil

	case *parser.IndexOperatorWriteNode:
		receiver, args, err := l.lowerIndexParts(n.Receiver, n.Arguments, n.Block)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: "[]", Args: args}
		return l.binaryAssign(read, n.BinaryOperator, value, pos, func(v Expr) Expr {
			writeArgs := append(append([]Expr{}, args...), v)
			return &Call{exprBase: base(pos), Receiver: receiver, Name: "[]=", Args: writeArgs}
		}), nil
	case *parser.IndexAndWriteNode:
		receiver, args, err := l.lowerIndexParts(n.Receiver, n.Arguments, n.Block)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: "[]", Args: args}
		return l.binaryAssign(read, "&&", value, pos, func(v Expr) Expr {
			writeArgs := append(append([]Expr{}, args...), v)
			return &Call{exprBase: base(pos), Receiver: receiver, Name: "[]=", Args: writeArgs}
		}), nil
	case *parser.IndexOrWriteNode:
		receiver, args, err := l.lowerIndexParts(n.Receiver, n.Arguments, n.Block)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: "[]", Args: args}
		return l.binaryAssign(read, "||", value, pos, func(v Expr) Expr {
			writeArgs := append(append([]Expr{}, args...), v)
			return &Call{exprBase: base(pos), Receiver: receiver, Name: "[]=", Args: writeArgs}
		}), nil
	case *parser.CallOperatorWriteNode:
		receiver, err := l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: n.ReadName}
		return l.binaryAssign(read, n.BinaryOperator, value, pos, func(v Expr) Expr {
			return &Call{exprBase: base(pos), Receiver: receiver, Name: n.WriteName, Args: []Expr{v}}
		}), nil
	case *parser.CallAndWriteNode:
		receiver, err := l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: n.ReadName}
		return l.binaryAssign(read, "&&", value, pos, func(v Expr) Expr {
			return &Call{exprBase: base(pos), Receiver: receiver, Name: n.WriteName, Args: []Expr{v}}
		}), nil
	case *parser.CallOrWriteNode:
		receiver, err := l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := &Call{exprBase: base(pos), Receiver: receiver, Name: n.ReadName}
		return l.binaryAssign(read, "||", value, pos, func(v Expr) Expr {
			return &Call{exprBase: base(pos), Receiver: receiver, Name: n.WriteName, Args: []Expr{v}}
		}), nil

	case *parser.CallNode:
		return l.lowerCall(n)

	case *parser.BeginNode:
		return l.lowerBegin(n)

	case *parser.RescueModifierNode:
		expression, err := l.lowerExpr(n.Expression)
		if err != nil {
			return nil, err
		}
		fallback, err := l.lowerExpr(n.RescueExpression)
		if err != nil {
			return nil, err
		}
		return &BeginExpr{
			exprBase: base(pos),
			Body:     []Expr{expression},
			Rescues:  []RescueClause{{Pos: pos, Body: []Expr{fallback}}},
		}, nil
	case *parser.RetryNode:
		return &RetryExpr{exprBase: base(pos)}, nil
	case *parser.RedoNode:
		return &RedoExpr{exprBase: base(pos)}, nil
	case *parser.AliasGlobalVariableNode:
		return nil, l.errorf(n, "global variable aliases are not supported")
	case *parser.AliasMethodNode:
		newName, err := l.aliasName(n.NewName)
		if err != nil {
			return nil, err
		}
		oldName, err := l.aliasName(n.OldName)
		if err != nil {
			return nil, err
		}
		return &AliasExpr{exprBase: base(pos), NewName: newName, OldName: oldName}, nil
	case *parser.UndefNode:
		names := make([]string, 0, len(n.Names))
		for _, node := range n.Names {
			name, err := l.aliasName(node)
			if err != nil {
				return nil, err
			}
			names = append(names, name)
		}
		return &UndefExpr{exprBase: base(pos), Names: names}, nil

	case *parser.MultiWriteNode:
		return l.lowerMultiWrite(n)
	case *parser.ConstantPathOperatorWriteNode:
		name, parent, err := l.lowerConstantPathTarget(n.Target)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return l.binaryAssign(
			&ConstantPathRead{exprBase: base(pos), Parent: parent, Name: name},
			n.BinaryOperator, value, pos, func(v Expr) Expr {
				return &ConstantPathWrite{exprBase: base(pos), Parent: parent, Name: name, Value: v}
			},
		), nil
	case *parser.ConstantPathAndWriteNode:
		name, parent, err := l.lowerConstantPathTarget(n.Target)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := softConstantRead(&ConstantPathRead{exprBase: base(pos), Parent: parent, Name: name}, pos)
		return &AndExpr{
			exprBase: base(pos),
			Left:     read,
			Right:    &ConstantPathWrite{exprBase: base(pos), Parent: parent, Name: name, Value: value},
		}, nil
	case *parser.ConstantPathOrWriteNode:
		name, parent, err := l.lowerConstantPathTarget(n.Target)
		if err != nil {
			return nil, err
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		read := softConstantRead(&ConstantPathRead{exprBase: base(pos), Parent: parent, Name: name}, pos)
		return &OrExpr{
			exprBase: base(pos),
			Left:     read,
			Right:    &ConstantPathWrite{exprBase: base(pos), Parent: parent, Name: name, Value: value},
		}, nil

	case *parser.ClassNode:
		return l.lowerClass(n)
	case *parser.ModuleNode:
		return l.lowerModule(n)
	case *parser.SingletonClassNode:
		return l.lowerSingletonClass(n)
	case *parser.LambdaNode:
		return l.lowerLambda(n)
	case *parser.SuperNode:
		return l.lowerSuper(n)
	case *parser.ForwardingSuperNode:
		return l.lowerForwardingSuper(n)
	case *parser.DefinedNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &Call{exprBase: base(pos), Name: "defined?", Args: []Expr{value}}, nil
	case *parser.MatchPredicateNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		pat, err := l.lowerPattern(n.Pattern)
		if err != nil {
			return nil, err
		}
		return &MatchPredicateExpr{exprBase: base(pos), Value: value, Pattern: pat}, nil
	case *parser.MatchRequiredNode:
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		pat, err := l.lowerPattern(n.Pattern)
		if err != nil {
			return nil, err
		}
		return &MatchRequiredExpr{exprBase: base(pos), Value: value, Pattern: pat}, nil
	case *parser.NumberedReferenceReadNode:
		return &GlobalRead{exprBase: base(pos), Name: fmt.Sprintf("$%d", n.Number)}, nil
	case *parser.BackReferenceReadNode:
		return &GlobalRead{exprBase: base(pos), Name: n.Name}, nil
	case *parser.MatchWriteNode:
		call, err := l.lowerCall(n.Call)
		if err != nil {
			return nil, err
		}
		targets := make([]MultiTarget, 0, len(n.Targets))
		for _, node := range n.Targets {
			target, err := l.lowerMultiTarget(node)
			if err != nil {
				return nil, err
			}
			targets = append(targets, target)
		}
		return &MatchWriteExpr{exprBase: base(pos), Call: call, Targets: targets}, nil
	case *parser.ItLocalVariableReadNode:
		return nil, l.errorf(n, "the 'it' block parameter is not supported yet")
	case *parser.SourceFileNode:
		return &StringLiteral{exprBase: base(pos), Value: l.source.File}, nil
	case *parser.SourceLineNode:
		return &IntegerLiteral{exprBase: base(pos), Value: int64(l.pos(n).Line)}, nil
	case *parser.SourceEncodingNode:
		return &Call{exprBase: base(pos), Name: "__ENCODING__"}, nil
	case *parser.ShareableConstantNode:
		return l.lowerExpr(n.Write)
	case *parser.ConstantPathWriteNode:
		if n.Target == nil || n.Target.Name == nil {
			return nil, l.errorf(n, "unsupported constant path target")
		}
		var parent Expr
		if n.Target.Parent != nil {
			var err error
			parent, err = l.lowerExpr(n.Target.Parent)
			if err != nil {
				return nil, err
			}
		}
		value, err := l.lowerExpr(n.Value)
		if err != nil {
			return nil, err
		}
		return &ConstantPathWrite{
			exprBase: base(pos),
			Parent:   parent,
			Name:     *n.Target.Name,
			Value:    value,
		}, nil
	case *parser.MissingNode:
		return nil, l.errorf(n, "incomplete expression")
	}

	return nil, l.unsupported(node)
}

func (l *lowerer) lowerBegin(n *parser.BeginNode) (Expr, error) {
	pos := l.pos(n)
	body, err := l.lowerStatements(n.Statements)
	if err != nil {
		return nil, err
	}
	var rescues []RescueClause
	for rescue := n.RescueClause; rescue != nil; rescue = rescue.Subsequent {
		clause := RescueClause{Pos: l.pos(rescue)}
		for _, exception := range rescue.Exceptions {
			if splat, ok := exception.(*parser.SplatNode); ok {
				value, err := l.lowerExpr(splat.Expression)
				if err != nil {
					return nil, err
				}
				clause.Classes = append(clause.Classes, &SplatArg{exprBase: base(l.pos(exception)), Value: value})
				continue
			}
			expr, err := l.lowerExpr(exception)
			if err != nil {
				return nil, err
			}
			clause.Classes = append(clause.Classes, expr)
		}
		if rescue.Reference != nil {
			target, err := l.lowerMultiTarget(rescue.Reference)
			if err != nil {
				return nil, err
			}
			clause.Target = &target
		}
		clause.Body, err = l.lowerStatements(rescue.Statements)
		if err != nil {
			return nil, err
		}
		rescues = append(rescues, clause)
	}
	var elseBody []Expr
	if n.ElseClause != nil {
		elseBody, err = l.lowerStatements(n.ElseClause.Statements)
		if err != nil {
			return nil, err
		}
	}
	var ensureBody []Expr
	if n.EnsureClause != nil {
		ensureBody, err = l.lowerStatements(n.EnsureClause.Statements)
		if err != nil {
			return nil, err
		}
	}
	if len(rescues) == 0 && len(elseBody) == 0 && len(ensureBody) == 0 {
		return statementsExpr(body, pos), nil
	}
	return &BeginExpr{
		exprBase: base(pos),
		Body:     body,
		Rescues:  rescues,
		Else:     elseBody,
		Ensure:   ensureBody,
	}, nil
}

func (l *lowerer) aliasName(node parser.Node) (string, error) {
	switch n := node.(type) {
	case *parser.SymbolNode:
		return n.Unescaped.Value, nil
	case *parser.LocalVariableReadNode:
		return n.Name, nil
	case *parser.ConstantReadNode:
		return n.Name, nil
	default:
		return "", l.errorf(node, "unsupported alias name: %s", nodeName(node))
	}
}

func (l *lowerer) lowerIf(n *parser.IfNode) (Expr, error) {
	pos := l.pos(n)
	cond, err := l.lowerExpr(n.Predicate)
	if err != nil {
		return nil, err
	}
	thenBody, err := l.lowerStatements(n.Statements)
	if err != nil {
		return nil, err
	}
	var elseExpr Expr
	switch subsequent := n.Subsequent.(type) {
	case nil:
	case *parser.ElseNode:
		elseBody, err := l.lowerStatements(subsequent.Statements)
		if err != nil {
			return nil, err
		}
		elseExpr = statementsExpr(elseBody, l.pos(subsequent))
	case *parser.IfNode:
		elseExpr, err = l.lowerIf(subsequent)
		if err != nil {
			return nil, err
		}
	default:
		return nil, l.unsupported(subsequent)
	}
	return &IfExpr{
		exprBase: base(pos),
		Cond:     cond,
		Then:     statementsExpr(thenBody, pos),
		Else:     elseExpr,
	}, nil
}

func (l *lowerer) lowerCase(n *parser.CaseNode) (Expr, error) {
	pos := l.pos(n)
	var subject Expr
	if n.Predicate != nil {
		var err error
		subject, err = l.lowerExpr(n.Predicate)
		if err != nil {
			return nil, err
		}
	}
	whens := make([]When, 0, len(n.Conditions))
	for _, condition := range n.Conditions {
		whenNode, ok := condition.(*parser.WhenNode)
		if !ok {
			return nil, l.unsupported(condition)
		}
		conds := make([]Expr, 0, len(whenNode.Conditions))
		for _, condNode := range whenNode.Conditions {
			if splat, ok := condNode.(*parser.SplatNode); ok {
				value, err := l.lowerExpr(splat.Expression)
				if err != nil {
					return nil, err
				}
				conds = append(conds, &SplatArg{exprBase: base(l.pos(splat)), Value: value})
				continue
			}
			cond, err := l.lowerExpr(condNode)
			if err != nil {
				return nil, err
			}
			conds = append(conds, cond)
		}
		body, err := l.lowerStatements(whenNode.Statements)
		if err != nil {
			return nil, err
		}
		whens = append(whens, When{Pos: l.pos(whenNode), Conds: conds, Body: body})
	}
	var elseExpr Expr
	if n.ElseClause != nil {
		body, err := l.lowerStatements(n.ElseClause.Statements)
		if err != nil {
			return nil, err
		}
		elseExpr = statementsExpr(body, l.pos(n.ElseClause))
	}
	return &CaseExpr{exprBase: base(pos), Subject: subject, Whens: whens, Else: elseExpr}, nil
}

func (l *lowerer) lowerCaseMatch(n *parser.CaseMatchNode) (Expr, error) {
	if n.Predicate == nil {
		return nil, l.errorf(n, "case/in without a subject is not supported")
	}
	subject, err := l.lowerExpr(n.Predicate)
	if err != nil {
		return nil, err
	}
	ins := make([]InClause, 0, len(n.Conditions))
	for _, condition := range n.Conditions {
		inNode, ok := condition.(*parser.InNode)
		if !ok {
			return nil, l.unsupported(condition)
		}
		clause := InClause{Pos: l.pos(inNode)}
		patternNode := inNode.Pattern
		switch guard := patternNode.(type) {
		case *parser.IfNode:
			if guard.Statements == nil || len(guard.Statements.Body) == 0 {
				return nil, l.errorf(guard, "unsupported pattern guard")
			}
			clause.Guard, err = l.lowerExpr(guard.Predicate)
			if err != nil {
				return nil, err
			}
			patternNode = guard.Statements.Body[0]
		case *parser.UnlessNode:
			if guard.Statements == nil || len(guard.Statements.Body) == 0 {
				return nil, l.errorf(guard, "unsupported pattern guard")
			}
			condition, err := l.lowerExpr(guard.Predicate)
			if err != nil {
				return nil, err
			}
			clause.Guard = &NotExpr{exprBase: base(l.pos(guard)), Value: condition}
			patternNode = guard.Statements.Body[0]
		}
		clause.Pattern, err = l.lowerPattern(patternNode)
		if err != nil {
			return nil, err
		}
		clause.Body, err = l.lowerStatements(inNode.Statements)
		if err != nil {
			return nil, err
		}
		ins = append(ins, clause)
	}
	var elseBody []Expr
	hasElse := n.ElseClause != nil
	if hasElse {
		elseBody, err = l.lowerStatements(n.ElseClause.Statements)
		if err != nil {
			return nil, err
		}
	}
	return &CaseMatchExpr{
		exprBase: base(l.pos(n)),
		Subject:  subject,
		Ins:      ins,
		Else:     elseBody,
		HasElse:  hasElse,
	}, nil
}

func (l *lowerer) lowerPattern(node parser.Node) (Pattern, error) {
	pos := l.pos(node)
	switch n := node.(type) {
	case *parser.IntegerNode, *parser.FloatNode, *parser.StringNode,
		*parser.InterpolatedStringNode, *parser.SymbolNode,
		*parser.RegularExpressionNode, *parser.InterpolatedRegularExpressionNode,
		*parser.NilNode, *parser.TrueNode,
		*parser.FalseNode, *parser.SelfNode, *parser.ConstantReadNode,
		*parser.ConstantPathNode, *parser.RangeNode:
		value, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		return &ValuePattern{patternBase: pattern(pos), Value: value}, nil

	case *parser.LocalVariableTargetNode:
		return &BindPattern{
			patternBase: pattern(pos),
			Target:      MultiTarget{Pos: pos, Kind: TargetLocal, Name: n.Name, Depth: int(n.Depth)},
		}, nil
	case *parser.LocalVariableReadNode:
		return &BindPattern{
			patternBase: pattern(pos),
			Target:      MultiTarget{Pos: pos, Kind: TargetLocal, Name: n.Name, Depth: int(n.Depth)},
		}, nil

	case *parser.CapturePatternNode:
		value, err := l.lowerPattern(n.Value)
		if err != nil {
			return nil, err
		}
		target, err := l.lowerMultiTarget(n.Target)
		if err != nil {
			return nil, err
		}
		return &CapturePattern{patternBase: pattern(pos), Value: value, Target: target}, nil

	case *parser.ArrayPatternNode:
		var constant Expr
		if n.Constant != nil {
			var err error
			constant, err = l.lowerExpr(n.Constant)
			if err != nil {
				return nil, err
			}
		}
		requireds := make([]Pattern, 0, len(n.Requireds))
		for _, element := range n.Requireds {
			pattern, err := l.lowerPattern(element)
			if err != nil {
				return nil, err
			}
			requireds = append(requireds, pattern)
		}
		var rest *Pattern
		hasRest := n.Rest != nil
		if hasRest {
			splat, ok := n.Rest.(*parser.SplatNode)
			if !ok {
				return nil, l.unsupported(n.Rest)
			}
			if splat.Expression != nil {
				pattern, err := l.lowerPattern(splat.Expression)
				if err != nil {
					return nil, err
				}
				rest = &pattern
			}
		}
		posts := make([]Pattern, 0, len(n.Posts))
		for _, element := range n.Posts {
			pattern, err := l.lowerPattern(element)
			if err != nil {
				return nil, err
			}
			posts = append(posts, pattern)
		}
		return &ArrayPattern{
			patternBase: pattern(pos),
			Constant:    constant,
			Requireds:   requireds,
			Rest:        rest,
			HasRest:     hasRest,
			Posts:       posts,
		}, nil

	case *parser.FindPatternNode:
		var constant Expr
		if n.Constant != nil {
			var err error
			constant, err = l.lowerExpr(n.Constant)
			if err != nil {
				return nil, err
			}
		}
		left, err := l.lowerSplatPattern(n.Left)
		if err != nil {
			return nil, err
		}
		requireds := make([]Pattern, 0, len(n.Requireds))
		for _, element := range n.Requireds {
			pattern, err := l.lowerPattern(element)
			if err != nil {
				return nil, err
			}
			requireds = append(requireds, pattern)
		}
		right, err := l.lowerSplatPattern(n.Right)
		if err != nil {
			return nil, err
		}
		return &FindPattern{
			patternBase: pattern(pos),
			Constant:    constant,
			Left:        left,
			Requireds:   requireds,
			Right:       right,
		}, nil

	case *parser.AlternationPatternNode:
		left, err := l.lowerPattern(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := l.lowerPattern(n.Right)
		if err != nil {
			return nil, err
		}
		return &AlternationPattern{patternBase: pattern(pos), Left: left, Right: right}, nil

	case *parser.PinnedVariableNode:
		value, err := l.lowerExpr(n.Variable)
		if err != nil {
			return nil, err
		}
		return &EqualPattern{patternBase: pattern(pos), Value: value}, nil
	case *parser.PinnedExpressionNode:
		value, err := l.lowerExpr(n.Expression)
		if err != nil {
			return nil, err
		}
		return &EqualPattern{patternBase: pattern(pos), Value: value}, nil
	case *parser.ImplicitNode:
		return l.lowerPattern(n.Value)

	case *parser.HashPatternNode:
		var constant Expr
		if n.Constant != nil {
			var err error
			constant, err = l.lowerExpr(n.Constant)
			if err != nil {
				return nil, err
			}
		}
		entries := make([]HashPatternEntry, 0, len(n.Elements))
		for _, element := range n.Elements {
			assoc, ok := element.(*parser.AssocNode)
			if !ok {
				return nil, l.errorf(element, "unsupported hash pattern element")
			}
			symbol, ok := assoc.Key.(*parser.SymbolNode)
			if !ok {
				return nil, l.errorf(assoc.Key, "only symbol keys are supported in hash patterns")
			}
			value, err := l.lowerPattern(assoc.Value)
			if err != nil {
				return nil, err
			}
			entries = append(entries, HashPatternEntry{Key: symbol.Unescaped.Value, Value: value})
		}
		var rest *MultiTarget
		if n.Rest != nil {
			splat, ok := n.Rest.(*parser.AssocSplatNode)
			if !ok {
				return nil, l.unsupported(n.Rest)
			}
			target, err := l.lowerMultiTarget(splat.Value)
			if err != nil {
				return nil, err
			}
			rest = &target
		}
		return &HashPattern{
			patternBase: pattern(pos),
			Constant:    constant,
			Entries:     entries,
			Rest:        rest,
		}, nil
	}
	return nil, l.errorf(node, "unsupported pattern: %s", nodeName(node))
}

func (l *lowerer) lowerDef(n *parser.DefNode) (Expr, error) {
	var receiver Expr
	if n.Receiver != nil {
		var err error
		receiver, err = l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
	}
	params, err := l.lowerParameters(n.Parameters)
	if err != nil {
		return nil, err
	}
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &DefExpr{
		exprBase: base(l.pos(n)),
		Name:     n.Name,
		Receiver: receiver,
		Params:   params,
		Body:     body,
		Locals:   append([]string(nil), n.Locals...),
	}, nil
}

func (l *lowerer) lowerParameters(params *parser.ParametersNode) ([]Param, error) {
	if params == nil {
		return nil, nil
	}
	var out []Param
	for _, node := range params.Requireds {
		switch required := node.(type) {
		case *parser.RequiredParameterNode:
			out = append(out, Param{Pos: l.pos(node), Name: required.Name, Kind: ParamRequired})
		case *parser.MultiTargetNode:
			lefts, rest, posts, err := l.lowerMultiTargetNode(required)
			if err != nil {
				return nil, err
			}
			out = append(out, Param{
				Pos:  l.pos(node),
				Kind: ParamDestructure,
				Destructure: &MultiTarget{
					Kind:  TargetNested,
					Lefts: lefts,
					Rest:  rest,
					Posts: posts,
				},
			})
		default:
			return nil, l.unsupported(node)
		}
	}
	for _, node := range params.Optionals {
		optional, ok := node.(*parser.OptionalParameterNode)
		if !ok {
			return nil, l.unsupported(node)
		}
		value, err := l.lowerExpr(optional.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, Param{Pos: l.pos(node), Name: optional.Name, Kind: ParamOptional, Default: value})
	}
	if params.Rest != nil {
		switch rest := params.Rest.(type) {
		case *parser.ImplicitRestNode:
		case *parser.RestParameterNode:
			name := ""
			if rest.Name != nil {
				name = *rest.Name
			}
			out = append(out, Param{Pos: l.pos(rest), Name: name, Kind: ParamRest})
		default:
			return nil, l.unsupported(params.Rest)
		}
	}
	for _, node := range params.Posts {
		post, ok := node.(*parser.RequiredParameterNode)
		if !ok {
			return nil, l.unsupported(node)
		}
		out = append(out, Param{Pos: l.pos(node), Name: post.Name, Kind: ParamPost})
	}
	for _, node := range params.Keywords {
		switch keyword := node.(type) {
		case *parser.RequiredKeywordParameterNode:
			out = append(out, Param{Pos: l.pos(node), Name: keyword.Name, Kind: ParamKeyword})
		case *parser.OptionalKeywordParameterNode:
			value, err := l.lowerExpr(keyword.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, Param{
				Pos:     l.pos(node),
				Name:    keyword.Name,
				Kind:    ParamOptionalKeyword,
				Default: value,
			})
		default:
			return nil, l.unsupported(node)
		}
	}
	if params.KeywordRest != nil {
		switch rest := params.KeywordRest.(type) {
		case *parser.ForwardingParameterNode:
			out = append(out,
				Param{Pos: l.pos(rest), Kind: ParamRest},
				Param{Pos: l.pos(rest), Kind: ParamKeywordRest},
				Param{Pos: l.pos(rest), Kind: ParamBlock},
			)
		case *parser.KeywordRestParameterNode:
			name := ""
			if rest.Name != nil {
				name = *rest.Name
			}
			out = append(out, Param{Pos: l.pos(rest), Name: name, Kind: ParamKeywordRest})
		default:
			return nil, l.unsupported(params.KeywordRest)
		}
	}
	if params.Block != nil {
		name := ""
		if params.Block.Name != nil {
			name = *params.Block.Name
		}
		out = append(out, Param{Pos: l.pos(params.Block), Name: name, Kind: ParamBlock})
	}
	return out, nil
}

func (l *lowerer) keywordHashPositional(node *parser.KeywordHashNode) bool {
	for _, element := range node.Elements {
		switch assoc := element.(type) {
		case *parser.AssocSplatNode:
			continue
		case *parser.AssocNode:
			if assoc.OperatorLoc != nil {
				return true
			}
			if _, ok := assoc.Key.(*parser.SymbolNode); !ok {
				return true
			}
		default:
			return false
		}
	}
	return false
}

func softConstantRead(read Expr, pos Position) Expr {
	return &BeginExpr{
		exprBase: base(pos),
		Body:     []Expr{read},
		Rescues: []RescueClause{{
			Pos:  pos,
			Body: []Expr{&FalseLiteral{exprBase: base(pos)}},
		}},
	}
}

// lowerConstantPathTarget lowers a constant path used as an assignment target.
// Reading the parent twice is only safe when it is a constant lookup, so
// dynamic parents are rejected instead of being evaluated twice.
func (l *lowerer) lowerConstantPathTarget(node parser.Node) (string, Expr, error) {
	path, ok := node.(*parser.ConstantPathNode)
	if !ok {
		return "", nil, l.errorf(node, "unsupported constant path target")
	}
	if path.Parent != nil && !isConstantPath(path.Parent) {
		return "", nil, l.errorf(path.Parent, "operator assignment for dynamic constant paths is not supported")
	}
	return l.lowerConstantTarget(node)
}

func isConstantPath(node parser.Node) bool {
	switch n := node.(type) {
	case nil:
		return true
	case *parser.ConstantReadNode:
		return true
	case *parser.ConstantPathNode:
		return n.Parent == nil || isConstantPath(n.Parent)
	default:
		return false
	}
}

func (l *lowerer) binaryAssign(read Expr, op string, value Expr, pos Position, write func(Expr) Expr) Expr {
	switch op {
	case "||":
		return &OrExpr{exprBase: base(pos), Left: read, Right: write(value)}
	case "&&":
		return &AndExpr{exprBase: base(pos), Left: read, Right: write(value)}
	default:
		computed := &Call{exprBase: base(pos), Receiver: read, Name: op, Args: []Expr{value}}
		return write(computed)
	}
}

func (l *lowerer) lowerCall(n *parser.CallNode) (Expr, error) {
	var receiver Expr
	if n.Receiver != nil {
		var err error
		receiver, err = l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
	}
	args, keywords, blockArg, forwardAll, err := l.lowerCallParts(n.Arguments)
	if err != nil {
		return nil, err
	}
	var block *Block
	switch b := n.Block.(type) {
	case nil:
	case *parser.BlockNode:
		block, err = l.lowerBlock(b)
		if err != nil {
			return nil, err
		}
	case *parser.BlockArgumentNode:
		if b.Expression == nil {
			blockArg = &BlockSlotExpr{exprBase: base(l.pos(b))}
		} else {
			blockArg, err = l.lowerExpr(b.Expression)
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, l.errorf(n.Block, "block argument forwarding is not supported yet")
	}
	return &Call{
		exprBase:       base(l.pos(n)),
		Receiver:       receiver,
		Name:           n.Name,
		Args:           args,
		Keywords:       keywords,
		ForwardAll:     forwardAll,
		SafeNavigation: n.IsSAFE_NAVIGATION(),
		Block:          block,
		BlockArg:       blockArg,
	}, nil
}

func (l *lowerer) lowerCallParts(args *parser.ArgumentsNode) ([]Expr, []KeywordArg, Expr, bool, error) {
	if args == nil {
		return nil, nil, nil, false, nil
	}
	var positional []Expr
	var keywords []KeywordArg
	var blockArg Expr
	forwardAll := false
	for _, node := range args.Arguments {
		switch arg := node.(type) {
		case *parser.SplatNode:
			value, err := l.lowerExpr(arg.Expression)
			if err != nil {
				return nil, nil, nil, false, err
			}
			positional = append(positional, &SplatArg{exprBase: base(l.pos(node)), Value: value})
		case *parser.KeywordHashNode:
			if l.keywordHashPositional(arg) {
				var pairs []HashPair
				for _, element := range arg.Elements {
					switch assoc := element.(type) {
					case *parser.AssocSplatNode:
						value, err := l.lowerExpr(assoc.Value)
						if err != nil {
							return nil, nil, nil, false, err
						}
						pairs = append(pairs, HashPair{Value: value})
					case *parser.AssocNode:
						key, err := l.lowerExpr(assoc.Key)
						if err != nil {
							return nil, nil, nil, false, err
						}
						value, err := l.lowerExpr(assoc.Value)
						if err != nil {
							return nil, nil, nil, false, err
						}
						pairs = append(pairs, HashPair{Key: key, Value: value})
					default:
						return nil, nil, nil, false, l.unsupported(element)
					}
				}
				positional = append(positional, &HashLiteral{exprBase: base(l.pos(arg)), Pairs: pairs})
				continue
			}
			for _, element := range arg.Elements {
				switch entry := element.(type) {
				case *parser.AssocNode:
					symbol, ok := entry.Key.(*parser.SymbolNode)
					if !ok {
						return nil, nil, nil, false, l.errorf(entry.Key, "only symbol keyword arguments are supported")
					}
					value, err := l.lowerExpr(entry.Value)
					if err != nil {
						return nil, nil, nil, false, err
					}
					keywords = append(keywords, KeywordArg{
						Key:   &SymbolLiteral{exprBase: base(l.pos(entry.Key)), Value: symbol.Unescaped.Value},
						Value: value,
					})
				case *parser.AssocSplatNode:
					value, err := l.lowerExpr(entry.Value)
					if err != nil {
						return nil, nil, nil, false, err
					}
					keywords = append(keywords, KeywordArg{Value: value})
				default:
					return nil, nil, nil, false, l.unsupported(element)
				}
			}
		case *parser.BlockArgumentNode:
			if arg.Expression == nil {
				blockArg = &BlockSlotExpr{exprBase: base(l.pos(arg))}
			} else {
				value, err := l.lowerExpr(arg.Expression)
				if err != nil {
					return nil, nil, nil, false, err
				}
				blockArg = value
			}
		case *parser.ForwardingArgumentsNode:
			forwardAll = true
		default:
			expr, err := l.lowerExpr(node)
			if err != nil {
				return nil, nil, nil, false, err
			}
			positional = append(positional, expr)
		}
	}
	return positional, keywords, blockArg, forwardAll, nil
}

func (l *lowerer) lowerClass(n *parser.ClassNode) (Expr, error) {
	name, outer, err := l.lowerConstantTarget(n.ConstantPath)
	if err != nil {
		return nil, err
	}
	var superclass Expr
	if n.Superclass != nil {
		superclass, err = l.lowerExpr(n.Superclass)
		if err != nil {
			return nil, err
		}
	}
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &ClassDef{
		exprBase:   base(l.pos(n)),
		Name:       name,
		Outer:      outer,
		Superclass: superclass,
		Body:       body,
		Locals:     append([]string(nil), n.Locals...),
	}, nil
}

func (l *lowerer) lowerModule(n *parser.ModuleNode) (Expr, error) {
	name, outer, err := l.lowerConstantTarget(n.ConstantPath)
	if err != nil {
		return nil, err
	}
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &ModuleDef{
		exprBase: base(l.pos(n)),
		Name:     name,
		Outer:    outer,
		Body:     body,
		Locals:   append([]string(nil), n.Locals...),
	}, nil
}

func (l *lowerer) lowerConstantTarget(node parser.Node) (string, Expr, error) {
	switch n := node.(type) {
	case *parser.ConstantReadNode:
		return n.Name, nil, nil
	case *parser.ConstantPathNode:
		if n.Parent == nil {
			if n.Name == nil {
				return "", nil, l.errorf(n, "dynamic constant name is not supported")
			}
			return *n.Name, &ObjectClassExpr{exprBase: base(l.pos(n))}, nil
		}
		parent, err := l.lowerExpr(n.Parent)
		if err != nil {
			return "", nil, err
		}
		if n.Name == nil {
			return "", nil, l.errorf(n, "dynamic constant name is not supported")
		}
		return *n.Name, parent, nil
	default:
		return "", nil, l.unsupported(node)
	}
}

func (l *lowerer) lowerSingletonClass(n *parser.SingletonClassNode) (Expr, error) {
	expression, err := l.lowerExpr(n.Expression)
	if err != nil {
		return nil, err
	}
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &SingletonClassDef{
		exprBase:   base(l.pos(n)),
		Expression: expression,
		Body:       body,
		Locals:     append([]string(nil), n.Locals...),
	}, nil
}

func (l *lowerer) lowerLambda(n *parser.LambdaNode) (Expr, error) {
	params, locals, err := l.lowerBlockParameters(n.Parameters)
	if err != nil {
		return nil, err
	}
	locals = dedupeStrings(append(locals, n.Locals...))
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &LambdaExpr{
		exprBase: base(l.pos(n)),
		Params:   params,
		Body:     body,
		Locals:   locals,
	}, nil
}

func (l *lowerer) lowerSuper(n *parser.SuperNode) (Expr, error) {
	args, keywords, blockArg, forwardAll, err := l.lowerCallParts(n.Arguments)
	if err != nil {
		return nil, err
	}
	if forwardAll {
		if len(args) > 0 || len(keywords) > 0 {
			return nil, l.errorf(n, "argument forwarding in super cannot be combined with other arguments")
		}
		var block *Block
		if n.Block != nil {
			blockNode, ok := n.Block.(*parser.BlockNode)
			if !ok {
				return nil, l.errorf(n.Block, "block argument forwarding is not supported yet")
			}
			block, err = l.lowerBlock(blockNode)
			if err != nil {
				return nil, err
			}
		}
		return &SuperExpr{
			exprBase:   base(l.pos(n)),
			Forwarding: true,
			Block:      block,
			BlockArg:   blockArg,
		}, nil
	}
	var block *Block
	if n.Block != nil {
		switch b := n.Block.(type) {
		case *parser.BlockNode:
			block, err = l.lowerBlock(b)
			if err != nil {
				return nil, err
			}
		case *parser.BlockArgumentNode:
			if b.Expression == nil {
				blockArg = &BlockSlotExpr{exprBase: base(l.pos(b))}
			} else {
				blockArg, err = l.lowerExpr(b.Expression)
				if err != nil {
					return nil, err
				}
			}
		default:
			return nil, l.errorf(n.Block, "block argument forwarding is not supported yet")
		}
	}
	return &SuperExpr{
		exprBase: base(l.pos(n)),
		Args:     args,
		Keywords: keywords,
		Block:    block,
		BlockArg: blockArg,
	}, nil
}

func (l *lowerer) lowerForwardingSuper(n *parser.ForwardingSuperNode) (Expr, error) {
	var block *Block
	if n.Block != nil {
		var err error
		block, err = l.lowerBlock(n.Block)
		if err != nil {
			return nil, err
		}
	}
	return &SuperExpr{exprBase: base(l.pos(n)), Forwarding: true, Block: block}, nil
}

func (l *lowerer) lowerMultiWrite(n *parser.MultiWriteNode) (Expr, error) {
	targets := make([]MultiTarget, 0, len(n.Lefts))
	for _, node := range n.Lefts {
		target, err := l.lowerMultiTarget(node)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	var rest *MultiTarget
	if n.Rest != nil {
		switch r := n.Rest.(type) {
		case *parser.ImplicitRestNode:
			// `a, = value` has a trailing comma but no rest binding.
		case *parser.SplatNode:
			if r.Expression != nil {
				target, err := l.lowerMultiTarget(r.Expression)
				if err != nil {
					return nil, err
				}
				rest = &target
			}
		default:
			return nil, l.unsupported(n.Rest)
		}
	}
	posts := make([]MultiTarget, 0, len(n.Rights))
	for _, node := range n.Rights {
		target, err := l.lowerMultiTarget(node)
		if err != nil {
			return nil, err
		}
		posts = append(posts, target)
	}
	if rest == nil && len(posts) > 0 {
		return nil, l.errorf(n, "post targets without a splat target are not supported")
	}
	value, err := l.lowerExpr(n.Value)
	if err != nil {
		return nil, err
	}
	return &MultiWrite{
		exprBase: base(l.pos(n)),
		Targets:  targets,
		Rest:     rest,
		Posts:    posts,
		Value:    value,
	}, nil
}

func (l *lowerer) lowerMultiTarget(node parser.Node) (MultiTarget, error) {
	pos := l.pos(node)
	switch target := node.(type) {
	case *parser.LocalVariableTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetLocal, Name: target.Name, Depth: int(target.Depth)}, nil
	case *parser.RequiredParameterNode:
		return MultiTarget{Pos: pos, Kind: TargetLocal, Name: target.Name}, nil
	case *parser.GlobalVariableTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetGlobal, Name: target.Name}, nil
	case *parser.InstanceVariableTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetInstance, Name: target.Name}, nil
	case *parser.ConstantTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetConstant, Name: target.Name}, nil
	case *parser.IndexTargetNode:
		receiver, args, err := l.lowerIndexParts(target.Receiver, target.Arguments, target.Block)
		if err != nil {
			return MultiTarget{}, err
		}
		return MultiTarget{Pos: pos, Kind: TargetIndex, Receiver: receiver, Args: args}, nil
	case *parser.CallTargetNode:
		receiver, err := l.lowerExpr(target.Receiver)
		if err != nil {
			return MultiTarget{}, err
		}
		return MultiTarget{Pos: pos, Kind: TargetCall, Name: target.Name, Receiver: receiver}, nil
	case *parser.MultiTargetNode:
		lefts, rest, posts, err := l.lowerMultiTargetNode(target)
		if err != nil {
			return MultiTarget{}, err
		}
		return MultiTarget{Pos: pos, Kind: TargetNested, Lefts: lefts, Rest: rest, Posts: posts}, nil
	default:
		return MultiTarget{}, l.errorf(node, "unsupported multiple assignment target: %s", nodeName(node))
	}
}

func (l *lowerer) lowerMultiTargetNode(multi *parser.MultiTargetNode) ([]MultiTarget, *MultiTarget, []MultiTarget, error) {
	lefts := make([]MultiTarget, 0, len(multi.Lefts))
	for _, left := range multi.Lefts {
		target, err := l.lowerMultiTarget(left)
		if err != nil {
			return nil, nil, nil, err
		}
		lefts = append(lefts, target)
	}
	var rest *MultiTarget
	if multi.Rest != nil {
		switch r := multi.Rest.(type) {
		case *parser.ImplicitRestNode:
		case *parser.SplatNode:
			if r.Expression != nil {
				target, err := l.lowerMultiTarget(r.Expression)
				if err != nil {
					return nil, nil, nil, err
				}
				rest = &target
			}
		default:
			return nil, nil, nil, l.unsupported(multi.Rest)
		}
	}
	posts := make([]MultiTarget, 0, len(multi.Rights))
	for _, right := range multi.Rights {
		target, err := l.lowerMultiTarget(right)
		if err != nil {
			return nil, nil, nil, err
		}
		posts = append(posts, target)
	}
	return lefts, rest, posts, nil
}

func (l *lowerer) lowerIndexParts(receiverNode parser.Node, arguments *parser.ArgumentsNode, block *parser.BlockArgumentNode) (Expr, []Expr, error) {
	if block != nil {
		return nil, nil, l.errorf(block, "block arguments in index assignment are not supported")
	}
	receiver, err := l.lowerExpr(receiverNode)
	if err != nil {
		return nil, nil, err
	}
	args, err := l.lowerArguments(arguments)
	if err != nil {
		return nil, nil, err
	}
	return receiver, args, nil
}

func (l *lowerer) lowerFor(n *parser.ForNode) (Expr, error) {
	targets, rest, posts, err := l.lowerForTargets(n.Index)
	if err != nil {
		return nil, err
	}
	collection, err := l.lowerExpr(n.Collection)
	if err != nil {
		return nil, err
	}
	body, err := l.lowerStatements(n.Statements)
	if err != nil {
		return nil, err
	}
	return &ForExpr{
		exprBase:   base(l.pos(n)),
		Targets:    targets,
		Rest:       rest,
		Posts:      posts,
		Collection: collection,
		Body:       body,
	}, nil
}

func (l *lowerer) lowerForTargets(node parser.Node) ([]MultiTarget, *MultiTarget, []MultiTarget, error) {
	multi, ok := node.(*parser.MultiTargetNode)
	if !ok {
		target, err := l.lowerMultiTarget(node)
		if err != nil {
			return nil, nil, nil, err
		}
		return []MultiTarget{target}, nil, nil, nil
	}
	targets := make([]MultiTarget, 0, len(multi.Lefts))
	for _, left := range multi.Lefts {
		target, err := l.lowerMultiTarget(left)
		if err != nil {
			return nil, nil, nil, err
		}
		targets = append(targets, target)
	}
	var rest *MultiTarget
	if multi.Rest != nil {
		switch r := multi.Rest.(type) {
		case *parser.ImplicitRestNode:
		case *parser.SplatNode:
			if r.Expression != nil {
				target, err := l.lowerMultiTarget(r.Expression)
				if err != nil {
					return nil, nil, nil, err
				}
				rest = &target
			}
		default:
			return nil, nil, nil, l.unsupported(multi.Rest)
		}
	}
	posts := make([]MultiTarget, 0, len(multi.Rights))
	for _, right := range multi.Rights {
		target, err := l.lowerMultiTarget(right)
		if err != nil {
			return nil, nil, nil, err
		}
		posts = append(posts, target)
	}
	return targets, rest, posts, nil
}

func (l *lowerer) lowerBlockParameters(node parser.Node) ([]Param, []string, error) {
	switch params := node.(type) {
	case nil:
		return nil, nil, nil
	case *parser.BlockParametersNode:
		lowered, err := l.lowerParameters(params.Parameters)
		if err != nil {
			return nil, nil, err
		}
		var locals []string
		for _, local := range params.Locals {
			if blockLocal, ok := local.(*parser.BlockLocalVariableNode); ok {
				locals = append(locals, blockLocal.Name)
			}
		}
		return lowered, locals, nil
	case *parser.NumberedParametersNode:
		lowered := make([]Param, 0, params.Maximum)
		for i := uint32(1); i <= uint32(params.Maximum); i++ {
			lowered = append(lowered, Param{Pos: l.pos(node), Name: fmt.Sprintf("_%d", i), Kind: ParamRequired})
		}
		return lowered, nil, nil
	case *parser.ItParametersNode:
		return nil, nil, l.errorf(node, "the 'it' block parameter is not supported yet")
	default:
		return nil, nil, l.unsupported(node)
	}
}

func (l *lowerer) lowerBlock(n *parser.BlockNode) (*Block, error) {
	params, declaredLocals, err := l.lowerBlockParameters(n.Parameters)
	if err != nil {
		return nil, err
	}
	locals := dedupeStrings(append(declaredLocals, n.Locals...))
	body, err := l.lowerBody(n.Body)
	if err != nil {
		return nil, err
	}
	return &Block{Pos: l.pos(n), Params: params, Body: body, Locals: locals}, nil
}

func (l *lowerer) lowerArguments(args *parser.ArgumentsNode) ([]Expr, error) {
	if args == nil {
		return nil, nil
	}
	out := make([]Expr, 0, len(args.Arguments))
	for _, node := range args.Arguments {
		switch n := node.(type) {
		case *parser.SplatNode:
			value, err := l.lowerExpr(n.Expression)
			if err != nil {
				return nil, err
			}
			out = append(out, &SplatArg{exprBase: base(l.pos(n)), Value: value})
			continue
		case *parser.KeywordHashNode:
			if !l.keywordHashPositional(n) {
				return nil, l.errorf(node, "keyword arguments are not supported here")
			}
			var pairs []HashPair
			for _, element := range n.Elements {
				switch assoc := element.(type) {
				case *parser.AssocSplatNode:
					value, err := l.lowerExpr(assoc.Value)
					if err != nil {
						return nil, err
					}
					pairs = append(pairs, HashPair{Value: value})
				case *parser.AssocNode:
					key, err := l.lowerExpr(assoc.Key)
					if err != nil {
						return nil, err
					}
					value, err := l.lowerExpr(assoc.Value)
					if err != nil {
						return nil, err
					}
					pairs = append(pairs, HashPair{Key: key, Value: value})
				default:
					return nil, l.unsupported(element)
				}
			}
			out = append(out, &HashLiteral{exprBase: base(l.pos(n)), Pairs: pairs})
			continue
		case *parser.BlockArgumentNode:
			return nil, l.errorf(node, "block argument forwarding is not supported here")
		case *parser.ForwardingArgumentsNode:
			return nil, l.errorf(node, "argument forwarding is not supported here")
		}
		expr, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		out = append(out, expr)
	}
	return out, nil
}

func (l *lowerer) lowerMultiValue(args *parser.ArgumentsNode) (Expr, error) {
	if args == nil || len(args.Arguments) == 0 {
		return nil, nil
	}
	if len(args.Arguments) == 1 {
		if splat, ok := args.Arguments[0].(*parser.SplatNode); ok {
			value, err := l.lowerExpr(splat.Expression)
			if err != nil {
				return nil, err
			}
			return &ArrayLiteral{
				exprBase: base(l.pos(splat)),
				Elements: []Expr{&SplatArg{exprBase: base(l.pos(splat)), Value: value}},
			}, nil
		}
		return l.lowerExpr(args.Arguments[0])
	}
	elements := make([]Expr, 0, len(args.Arguments))
	for _, node := range args.Arguments {
		if splat, ok := node.(*parser.SplatNode); ok {
			value, err := l.lowerExpr(splat.Expression)
			if err != nil {
				return nil, err
			}
			elements = append(elements, &SplatArg{exprBase: base(l.pos(node)), Value: value})
			continue
		}
		expr, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		elements = append(elements, expr)
	}
	return &ArrayLiteral{exprBase: base(l.pos(args)), Elements: elements}, nil
}

func (l *lowerer) lowerInteger(n *parser.IntegerNode, pos Position) (Expr, error) {
	start := n.Location.StartOffset
	end := start + n.Location.Length
	if start < 0 || end > len(l.source.text) || start >= end {
		return &IntegerLiteral{exprBase: base(pos), Value: n.Value}, nil
	}
	raw := string(l.source.text[start:end])
	text := strings.ReplaceAll(raw, "_", "")
	sign := ""
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		sign, text = text[:1], text[1:]
	}
	radix := 0
	if strings.HasPrefix(text, "0d") || strings.HasPrefix(text, "0D") {
		text = text[2:]
		radix = 10
	}
	value, ok := new(big.Int).SetString(sign+text, radix)
	if !ok {
		return nil, l.errorf(n, "invalid integer literal %q", raw)
	}
	if value.IsInt64() {
		return &IntegerLiteral{exprBase: base(pos), Value: value.Int64()}, nil
	}
	negative := value.Sign() < 0
	magnitude := new(big.Int).Abs(value).String()
	if len(magnitude) > 255 {
		return nil, l.errorf(n, "integer literal is too large")
	}
	return &BigIntLiteral{exprBase: base(pos), Digits: magnitude, Negative: negative}, nil
}

func (l *lowerer) lowerInterpolatedString(n *parser.InterpolatedStringNode) (Expr, error) {
	parts, err := l.lowerStringParts(n.Parts)
	if err != nil {
		return nil, err
	}
	return &InterpolatedString{exprBase: base(l.pos(n)), Parts: parts}, nil
}

func (l *lowerer) lowerSplatPattern(node parser.Node) (*Pattern, error) {
	splat, ok := node.(*parser.SplatNode)
	if !ok {
		if node == nil {
			return nil, nil
		}
		return nil, l.unsupported(node)
	}
	if splat == nil || splat.Expression == nil {
		return nil, nil
	}
	pattern, err := l.lowerPattern(splat.Expression)
	if err != nil {
		return nil, err
	}
	return &pattern, nil
}

func (l *lowerer) lowerStringParts(parts []parser.Node) ([]Expr, error) {
	out := make([]Expr, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case *parser.StringNode:
			out = append(out, &StringLiteral{exprBase: base(l.pos(p)), Value: p.Unescaped.Value})
		case *parser.InterpolatedStringNode:
			nested, err := l.lowerStringParts(p.Parts)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
		case *parser.EmbeddedStatementsNode:
			body, err := l.lowerStatements(p.Statements)
			if err != nil {
				return nil, err
			}
			if len(body) == 0 {
				out = append(out, &StringLiteral{exprBase: base(l.pos(p)), Value: ""})
			} else {
				out = append(out, statementsExpr(body, l.pos(p)))
			}
		case *parser.EmbeddedVariableNode:
			expr, err := l.lowerExpr(p.Variable)
			if err != nil {
				return nil, err
			}
			out = append(out, expr)
		default:
			return nil, l.unsupported(part)
		}
	}
	return out, nil
}

type regexFlagNode interface {
	IsIGNORE_CASE() bool
	IsEXTENDED() bool
	IsMULTI_LINE() bool
}

func regexFlags(node regexFlagNode) string {
	flags := ""
	if node.IsIGNORE_CASE() {
		flags += "i"
	}
	if node.IsMULTI_LINE() {
		flags += "m"
	}
	if node.IsEXTENDED() {
		flags += "x"
	}
	return flags
}

func statementsExpr(body []Expr, pos Position) Expr {
	switch len(body) {
	case 0:
		return &NilLiteral{exprBase: base(pos)}
	case 1:
		return body[0]
	default:
		return &Statements{exprBase: base(pos), Body: body}
	}
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
