package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

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
		return &IntegerLiteral{exprBase: base(pos), Value: n.Value}, nil
	case *parser.FloatNode:
		return &FloatLiteral{exprBase: base(pos), Value: n.Value}, nil
	case *parser.StringNode:
		return &StringLiteral{exprBase: base(pos), Value: n.Unescaped.Value}, nil
	case *parser.SymbolNode:
		return &SymbolLiteral{exprBase: base(pos), Value: n.Unescaped.Value}, nil

	case *parser.InterpolatedStringNode:
		return l.lowerInterpolatedString(n)
	case *parser.InterpolatedSymbolNode:
		return nil, l.errorf(n, "interpolated symbols are not supported yet")

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
			if _, ok := element.(*parser.SplatNode); ok {
				return nil, l.errorf(element, "array splats are not supported yet")
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
			assoc, ok := element.(*parser.AssocNode)
			if !ok {
				return nil, l.errorf(element, "hash splats are not supported yet")
			}
			key, err := l.lowerExpr(assoc.Key)
			if err != nil {
				return nil, err
			}
			value, err := l.lowerExpr(assoc.Value)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, HashPair{Key: key, Value: value})
		}
		return &HashLiteral{exprBase: base(pos), Pairs: pairs}, nil

	case *parser.RangeNode:
		if n.Left == nil || n.Right == nil {
			return nil, l.errorf(n, "beginless and endless ranges are not supported yet")
		}
		low, err := l.lowerExpr(n.Left)
		if err != nil {
			return nil, err
		}
		high, err := l.lowerExpr(n.Right)
		if err != nil {
			return nil, err
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
		if n.IsBEGIN_MODIFIER() {
			return nil, l.errorf(n, "begin/end while is not supported yet")
		}
		cond, err := l.lowerExpr(n.Predicate)
		if err != nil {
			return nil, err
		}
		body, err := l.lowerStatements(n.Statements)
		if err != nil {
			return nil, err
		}
		return &WhileExpr{exprBase: base(pos), Cond: cond, Body: statementsExpr(body, pos)}, nil

	case *parser.UntilNode:
		if n.IsBEGIN_MODIFIER() {
			return nil, l.errorf(n, "begin/end until is not supported yet")
		}
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
		value, err := l.lowerSingleArgument(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &ReturnExpr{exprBase: base(pos), Value: value}, nil
	case *parser.BreakNode:
		value, err := l.lowerSingleArgument(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &BreakExpr{exprBase: base(pos), Value: value}, nil
	case *parser.NextNode:
		value, err := l.lowerSingleArgument(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &NextExpr{exprBase: base(pos), Value: value}, nil

	case *parser.YieldNode:
		args, err := l.lowerArguments(n.Arguments)
		if err != nil {
			return nil, err
		}
		return &YieldExpr{exprBase: base(pos), Args: args}, nil

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
	case *parser.ConstantOperatorWriteNode, *parser.ConstantAndWriteNode,
		*parser.ConstantOrWriteNode, *parser.CallOperatorWriteNode,
		*parser.CallAndWriteNode, *parser.CallOrWriteNode,
		*parser.IndexOperatorWriteNode, *parser.IndexAndWriteNode, *parser.IndexOrWriteNode:
		return nil, l.errorf(n, "operator assignment for this target is not supported yet")

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
		return nil, l.errorf(n, "defined? is not supported yet")
	case *parser.RegularExpressionNode, *parser.InterpolatedRegularExpressionNode,
		*parser.MatchPredicateNode, *parser.MatchRequiredNode:
		return nil, l.errorf(n, "regular expressions are not supported yet")
	case *parser.NumberedReferenceReadNode, *parser.ItLocalVariableReadNode:
		return nil, l.errorf(n, "numbered block parameters are not supported yet")
	case *parser.SourceFileNode:
		return nil, l.errorf(n, "__FILE__ is not supported yet")
	case *parser.SourceLineNode:
		return nil, l.errorf(n, "__LINE__ is not supported yet")
	case *parser.SourceEncodingNode:
		return nil, l.errorf(n, "__ENCODING__ is not supported yet")
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
			if _, ok := exception.(*parser.SplatNode); ok {
				return nil, l.errorf(exception, "splat rescue classes are not supported yet")
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
		*parser.SymbolNode, *parser.NilNode, *parser.TrueNode,
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
		if n.Rest != nil {
			return nil, l.errorf(n.Rest, "array pattern rest is not supported yet")
		}
		var constant Expr
		if n.Constant != nil {
			var err error
			constant, err = l.lowerExpr(n.Constant)
			if err != nil {
				return nil, err
			}
		}
		elements := make([]Pattern, 0, len(n.Requireds)+len(n.Posts))
		for _, element := range n.Requireds {
			pattern, err := l.lowerPattern(element)
			if err != nil {
				return nil, err
			}
			elements = append(elements, pattern)
		}
		for _, element := range n.Posts {
			pattern, err := l.lowerPattern(element)
			if err != nil {
				return nil, err
			}
			elements = append(elements, pattern)
		}
		return &ArrayPattern{patternBase: pattern(pos), Constant: constant, Elements: elements}, nil

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
		return nil, l.errorf(n, "hash patterns are not supported yet")
	case *parser.FindPatternNode:
		return nil, l.errorf(n, "find patterns are not supported yet")
	case *parser.AlternationPatternNode:
		return nil, l.errorf(n, "alternation patterns are not supported yet")
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
		required, ok := node.(*parser.RequiredParameterNode)
		if !ok {
			return nil, l.unsupported(node)
		}
		out = append(out, Param{Pos: l.pos(node), Name: required.Name, Kind: ParamRequired})
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
		rest, ok := params.Rest.(*parser.RestParameterNode)
		if !ok {
			return nil, l.unsupported(params.Rest)
		}
		if rest.Name == nil {
			return nil, l.errorf(rest, "anonymous rest parameters are not supported yet")
		}
		out = append(out, Param{Pos: l.pos(rest), Name: *rest.Name, Kind: ParamRest})
	}
	if len(params.Posts) > 0 {
		return nil, l.errorf(params.Posts[0], "post parameters are not supported yet")
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
		rest, ok := params.KeywordRest.(*parser.KeywordRestParameterNode)
		if !ok {
			return nil, l.unsupported(params.KeywordRest)
		}
		if rest.Name == nil {
			return nil, l.errorf(rest, "anonymous keyword rest parameters are not supported yet")
		}
		out = append(out, Param{Pos: l.pos(rest), Name: *rest.Name, Kind: ParamKeywordRest})
	}
	if params.Block != nil {
		if params.Block.Name == nil {
			return nil, l.errorf(params.Block, "anonymous block parameters are not supported yet")
		}
		out = append(out, Param{Pos: l.pos(params.Block), Name: *params.Block.Name, Kind: ParamBlock})
	}
	return out, nil
}

func (l *lowerer) lowerCall(n *parser.CallNode) (Expr, error) {
	if n.IsSAFE_NAVIGATION() {
		return nil, l.errorf(n, "safe navigation is not supported yet")
	}
	var receiver Expr
	if n.Receiver != nil {
		var err error
		receiver, err = l.lowerExpr(n.Receiver)
		if err != nil {
			return nil, err
		}
	}
	args, keywords, keywordSplat, blockArg, err := l.lowerCallParts(n.Arguments)
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
			return nil, l.errorf(b, "anonymous block forwarding is not supported yet")
		}
		blockArg, err = l.lowerExpr(b.Expression)
		if err != nil {
			return nil, err
		}
	default:
		return nil, l.errorf(n.Block, "block argument forwarding is not supported yet")
	}
	return &Call{
		exprBase:     base(l.pos(n)),
		Receiver:     receiver,
		Name:         n.Name,
		Args:         args,
		Keywords:     keywords,
		KeywordSplat: keywordSplat,
		Block:        block,
		BlockArg:     blockArg,
	}, nil
}

func (l *lowerer) lowerCallParts(args *parser.ArgumentsNode) ([]Expr, []HashPair, Expr, Expr, error) {
	if args == nil {
		return nil, nil, nil, nil, nil
	}
	var positional []Expr
	var keywords []HashPair
	var keywordSplat Expr
	var blockArg Expr
	for _, node := range args.Arguments {
		switch arg := node.(type) {
		case *parser.SplatNode:
			value, err := l.lowerExpr(arg.Expression)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			positional = append(positional, &SplatArg{exprBase: base(l.pos(node)), Value: value})
		case *parser.KeywordHashNode:
			for _, element := range arg.Elements {
				switch entry := element.(type) {
				case *parser.AssocNode:
					symbol, ok := entry.Key.(*parser.SymbolNode)
					if !ok {
						return nil, nil, nil, nil, l.errorf(entry.Key, "only symbol keyword arguments are supported")
					}
					value, err := l.lowerExpr(entry.Value)
					if err != nil {
						return nil, nil, nil, nil, err
					}
					keywords = append(keywords, HashPair{
						Key:   &SymbolLiteral{exprBase: base(l.pos(entry.Key)), Value: symbol.Unescaped.Value},
						Value: value,
					})
				case *parser.AssocSplatNode:
					if keywordSplat != nil {
						return nil, nil, nil, nil, l.errorf(entry, "multiple keyword splats are not supported")
					}
					value, err := l.lowerExpr(entry.Value)
					if err != nil {
						return nil, nil, nil, nil, err
					}
					keywordSplat = value
				default:
					return nil, nil, nil, nil, l.unsupported(element)
				}
			}
		case *parser.BlockArgumentNode:
			if arg.Expression == nil {
				return nil, nil, nil, nil, l.errorf(arg, "anonymous block forwarding is not supported yet")
			}
			value, err := l.lowerExpr(arg.Expression)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			blockArg = value
		case *parser.ForwardingArgumentsNode:
			return nil, nil, nil, nil, l.errorf(node, "argument forwarding is not supported yet")
		default:
			expr, err := l.lowerExpr(node)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			positional = append(positional, expr)
		}
	}
	return positional, keywords, keywordSplat, blockArg, nil
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
			return *n.Name, nil, nil
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
	args, keywords, keywordSplat, blockArg, err := l.lowerCallParts(n.Arguments)
	if err != nil {
		return nil, err
	}
	if len(keywords) > 0 || keywordSplat != nil {
		return nil, l.errorf(n, "keyword arguments to super are not supported yet")
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
		exprBase: base(l.pos(n)),
		Args:     args,
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
		splat, ok := n.Rest.(*parser.SplatNode)
		if !ok {
			return nil, l.unsupported(n.Rest)
		}
		if splat.Expression != nil {
			target, err := l.lowerMultiTarget(splat.Expression)
			if err != nil {
				return nil, err
			}
			rest = &target
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
	case *parser.GlobalVariableTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetGlobal, Name: target.Name}, nil
	case *parser.InstanceVariableTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetInstance, Name: target.Name}, nil
	case *parser.ConstantTargetNode:
		return MultiTarget{Pos: pos, Kind: TargetConstant, Name: target.Name}, nil
	default:
		return MultiTarget{}, l.errorf(node, "unsupported multiple assignment target: %s", nodeName(node))
	}
}

func (l *lowerer) lowerFor(n *parser.ForNode) (Expr, error) {
	target, err := l.lowerMultiTarget(n.Index)
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
		Target:     target,
		Collection: collection,
		Body:       body,
	}, nil
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
	case *parser.NumberedParametersNode, *parser.ItParametersNode:
		return nil, nil, l.errorf(node, "numbered block parameters are not supported yet")
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
		switch node.(type) {
		case *parser.SplatNode:
			return nil, l.errorf(node, "splat arguments are not supported yet")
		case *parser.KeywordHashNode:
			return nil, l.errorf(node, "keyword arguments are not supported yet")
		case *parser.BlockArgumentNode:
			return nil, l.errorf(node, "block argument forwarding is not supported yet")
		case *parser.ForwardingArgumentsNode:
			return nil, l.errorf(node, "argument forwarding is not supported yet")
		}
		expr, err := l.lowerExpr(node)
		if err != nil {
			return nil, err
		}
		out = append(out, expr)
	}
	return out, nil
}

func (l *lowerer) lowerSingleArgument(args *parser.ArgumentsNode) (Expr, error) {
	if args == nil || len(args.Arguments) == 0 {
		return nil, nil
	}
	if len(args.Arguments) > 1 {
		return nil, l.errorf(args, "multiple return values are not supported yet")
	}
	return l.lowerExpr(args.Arguments[0])
}

func (l *lowerer) lowerInterpolatedString(n *parser.InterpolatedStringNode) (Expr, error) {
	parts := make([]Expr, 0, len(n.Parts))
	for _, part := range n.Parts {
		switch p := part.(type) {
		case *parser.StringNode:
			parts = append(parts, &StringLiteral{exprBase: base(l.pos(p)), Value: p.Unescaped.Value})
		case *parser.EmbeddedStatementsNode:
			body, err := l.lowerStatements(p.Statements)
			if err != nil {
				return nil, err
			}
			if len(body) == 0 {
				parts = append(parts, &StringLiteral{exprBase: base(l.pos(p)), Value: ""})
			} else {
				parts = append(parts, statementsExpr(body, l.pos(p)))
			}
		case *parser.EmbeddedVariableNode:
			expr, err := l.lowerExpr(p.Variable)
			if err != nil {
				return nil, err
			}
			parts = append(parts, expr)
		default:
			return nil, l.unsupported(part)
		}
	}
	return &InterpolatedString{exprBase: base(l.pos(n)), Parts: parts}, nil
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
