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
		if n.RescueClause != nil || n.ElseClause != nil || n.EnsureClause != nil {
			return nil, l.errorf(n, "begin/rescue/ensure is not supported yet")
		}
		return l.lowerStatements(n.Statements)
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
		if n.RescueClause != nil || n.ElseClause != nil || n.EnsureClause != nil {
			return nil, l.errorf(n, "begin/rescue/ensure is not supported yet")
		}
		body, err := l.lowerStatements(n.Statements)
		if err != nil {
			return nil, err
		}
		return statementsExpr(body, pos), nil

	case *parser.MultiWriteNode:
		return nil, l.errorf(n, "multiple assignment is not supported yet")
	case *parser.InstanceVariableOperatorWriteNode, *parser.InstanceVariableAndWriteNode,
		*parser.InstanceVariableOrWriteNode, *parser.ConstantOperatorWriteNode,
		*parser.ConstantAndWriteNode, *parser.ConstantOrWriteNode,
		*parser.CallOperatorWriteNode, *parser.CallAndWriteNode, *parser.CallOrWriteNode,
		*parser.IndexOperatorWriteNode, *parser.IndexAndWriteNode, *parser.IndexOrWriteNode:
		return nil, l.errorf(n, "operator assignment for this target is not supported yet")

	case *parser.ClassNode, *parser.ModuleNode, *parser.SingletonClassNode:
		return nil, l.errorf(n, "classes and modules are not supported yet")
	case *parser.LambdaNode:
		return nil, l.errorf(n, "lambda literals are not supported yet")
	case *parser.SuperNode, *parser.ForwardingSuperNode:
		return nil, l.errorf(n, "super is not supported yet")
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

func (l *lowerer) lowerDef(n *parser.DefNode) (Expr, error) {
	if n.Receiver != nil {
		return nil, l.errorf(n, "singleton method definitions are not supported yet")
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
	if len(params.Keywords) > 0 {
		return nil, l.errorf(params.Keywords[0], "keyword parameters are not supported yet")
	}
	if params.KeywordRest != nil {
		return nil, l.errorf(params.KeywordRest, "keyword rest parameters are not supported yet")
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
	args, err := l.lowerArguments(n.Arguments)
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
	default:
		return nil, l.errorf(n.Block, "block argument forwarding is not supported yet")
	}
	return &Call{
		exprBase: base(l.pos(n)),
		Receiver: receiver,
		Name:     n.Name,
		Args:     args,
		Block:    block,
	}, nil
}

func (l *lowerer) lowerBlock(n *parser.BlockNode) (*Block, error) {
	var params []Param
	var declaredLocals []string
	switch p := n.Parameters.(type) {
	case nil:
	case *parser.BlockParametersNode:
		var err error
		params, err = l.lowerParameters(p.Parameters)
		if err != nil {
			return nil, err
		}
		for _, local := range p.Locals {
			if blockLocal, ok := local.(*parser.BlockLocalVariableNode); ok {
				declaredLocals = append(declaredLocals, blockLocal.Name)
			}
		}
	case *parser.NumberedParametersNode, *parser.ItParametersNode:
		return nil, l.errorf(n, "numbered block parameters are not supported yet")
	default:
		return nil, l.unsupported(n.Parameters)
	}
	locals := append([]string(nil), declaredLocals...)
	locals = append(locals, n.Locals...)
	locals = dedupeStrings(locals)
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
