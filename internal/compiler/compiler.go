package compiler

import (
	"fmt"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

// Compiler lowers the internal IR to mruby 4.0.0 bytecode.
type Compiler struct {
	source     *frontend.Source
	err        error
	forCounter int
}

// Compile compiles a lowered program into a top-level irep.
func Compile(program *frontend.Program, file string, src []byte) (*bytecode.Irep, error) {
	if program == nil {
		return nil, fmt.Errorf("compiler: nil program")
	}
	c := &Compiler{source: frontend.NewSource(file, src)}
	top := c.compileTop(program)
	if c.err != nil {
		return nil, c.err
	}
	return top, nil
}

func (c *Compiler) fail(pos frontend.Position, format string, args ...any) {
	if c.err != nil {
		return
	}
	c.err = &frontend.Error{
		File:     c.source.File,
		Position: pos,
		Message:  fmt.Sprintf(format, args...),
	}
}

func (c *Compiler) failExpr(expr frontend.Expr, format string, args ...any) {
	pos := frontend.Position{Line: 1, Column: 1}
	if expr != nil {
		pos = expr.Position()
	}
	c.fail(pos, format, args...)
}

func (c *Compiler) compileTop(program *frontend.Program) *bytecode.Irep {
	s := c.newScope(nil, scopeTop, nil, program.Locals)
	if c.err != nil {
		return nil
	}
	s.markLine(program.Pos)

	sp0 := s.sp
	dst := s.alloc(1)
	c.compileBody(s, program.Body, dst)
	s.emit(bytecode.OpRETURN, uint32(dst))
	s.emit(bytecode.OpSTOP)
	s.sp = sp0

	if err := s.finish(); err != nil {
		c.fail(program.Pos, "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileBody(s *scope, body []frontend.Expr, dst int) {
	if len(body) == 0 {
		s.emit(bytecode.OpLOADNIL, uint32(dst))
		return
	}
	sp0 := s.sp
	for i, expr := range body {
		if i == len(body)-1 {
			c.compileExpr(s, expr, dst)
			continue
		}
		c.compileStmt(s, expr)
		s.sp = sp0
	}
}

// compileAt compiles expr into dst with sp temporarily set so that dst is the
// top temporary. Registers above dst are reserved by the caller but have not
// been evaluated yet, so a nested call can reuse them and avoid a copy.
func (c *Compiler) compileAt(s *scope, expr frontend.Expr, dst, reservedSP int) {
	s.setSP(dst + 1)
	c.compileExpr(s, expr, dst)
	s.setSP(reservedSP)
}

// compileStmt compiles an expression whose value is discarded. Cases that
// would otherwise materialize a temporary just to drop it skip that copy.
func (c *Compiler) compileStmt(s *scope, expr frontend.Expr) {
	if c.err != nil || expr == nil {
		return
	}
	s.markLine(expr.Position())
	sp0 := s.sp
	defer func() { s.sp = sp0 }()

	switch e := expr.(type) {
	case *frontend.NilLiteral, *frontend.TrueLiteral, *frontend.FalseLiteral,
		*frontend.IntegerLiteral, *frontend.FloatLiteral, *frontend.BigIntLiteral,
		*frontend.StringLiteral, *frontend.SymbolLiteral,
		*frontend.LocalRead, *frontend.SelfExpr:
		// Literals and local reads have no side effects.
		return
	case *frontend.LocalWrite:
		c.compileLocalWrite(s, e, 0, false)
	case *frontend.Call:
		c.compileCallDiscard(s, e)
	case *frontend.Statements:
		for _, stmt := range e.Body {
			c.compileStmt(s, stmt)
		}
	default:
		tmp := s.alloc(1)
		c.compileExpr(s, expr, tmp)
	}
}

func (c *Compiler) compileMethod(parent *scope, def *frontend.DefExpr) *bytecode.Irep {
	s := c.newScope(parent, scopeMethod, def.Params, def.Locals)
	if c.err != nil {
		return nil
	}
	s.markLine(def.Position())
	aspec, ainfo := s.enterAspec()
	s.methodInfo = &methodInfo{
		aspec:       aspec,
		ainfo:       ainfo,
		blockSlot:   s.blockSlot,
		keywordSlot: s.keywordSlot,
		restSlot:    s.restSlot,
	}
	for _, param := range s.params {
		switch param.Kind {
		case frontend.ParamKeyword, frontend.ParamOptionalKeyword:
			s.methodInfo.keywordParams = append(s.methodInfo.keywordParams, param)
		case frontend.ParamKeywordRest:
			s.methodInfo.hasKeywordRest = true
		}
	}
	s.emit(bytecode.OpENTER, aspec)
	c.emitOptionalDefaults(s)
	c.emitKeywordInit(s)
	c.emitParamDestructure(s)
	c.emitBlockParamCopy(s)

	sp0 := s.sp
	if len(def.Body) == 0 {
		s.emit(bytecode.OpRETNIL)
	} else {
		dst := s.alloc(1)
		c.compileBody(s, def.Body, dst)
		s.emit(bytecode.OpRETURN, uint32(dst))
		s.sp = sp0
	}

	if err := s.finish(); err != nil {
		c.fail(def.Position(), "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileBlock(parent *scope, block *frontend.Block) *bytecode.Irep {
	s := c.newScope(parent, scopeBlock, block.Params, block.Locals)
	if c.err != nil {
		return nil
	}
	s.markLine(block.Pos)
	aspec, _ := s.enterAspec()
	s.emit(bytecode.OpENTER, aspec)
	c.emitOptionalDefaults(s)
	c.emitKeywordInit(s)
	c.emitParamDestructure(s)
	c.emitBlockParamCopy(s)

	sp0 := s.sp
	redoLabel := s.asm.newLabel()
	s.asm.bind(redoLabel)
	prevRedo, prevHasRedo := s.redoTarget, s.hasRedo
	s.redoTarget, s.hasRedo = redoLabel, true
	if len(block.Body) == 0 {
		s.emit(bytecode.OpRETNIL)
	} else {
		dst := s.alloc(1)
		c.compileBody(s, block.Body, dst)
		s.emit(bytecode.OpRETURN, uint32(dst))
		s.sp = sp0
	}
	s.redoTarget, s.hasRedo = prevRedo, prevHasRedo

	if err := s.finish(); err != nil {
		c.fail(block.Pos, "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileLambda(parent *scope, lambda *frontend.LambdaExpr) *bytecode.Irep {
	s := c.newScope(parent, scopeLambda, lambda.Params, lambda.Locals)
	if c.err != nil {
		return nil
	}
	s.markLine(lambda.Position())
	aspec, _ := s.enterAspec()
	s.emit(bytecode.OpENTER, aspec)
	c.emitOptionalDefaults(s)
	c.emitKeywordInit(s)
	c.emitParamDestructure(s)
	c.emitBlockParamCopy(s)

	sp0 := s.sp
	redoLabel := s.asm.newLabel()
	s.asm.bind(redoLabel)
	prevRedo, prevHasRedo := s.redoTarget, s.hasRedo
	s.redoTarget, s.hasRedo = redoLabel, true
	if len(lambda.Body) == 0 {
		s.emit(bytecode.OpRETNIL)
	} else {
		dst := s.alloc(1)
		c.compileBody(s, lambda.Body, dst)
		s.emit(bytecode.OpRETURN, uint32(dst))
		s.sp = sp0
	}
	s.redoTarget, s.hasRedo = prevRedo, prevHasRedo

	if err := s.finish(); err != nil {
		c.fail(lambda.Position(), "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileClassBody(parent *scope, locals []string, body []frontend.Expr, pos frontend.Position) *bytecode.Irep {
	s := c.newScope(parent, scopeClass, nil, locals)
	if c.err != nil {
		return nil
	}
	s.markLine(pos)
	sp0 := s.sp
	dst := s.alloc(1)
	c.compileBody(s, body, dst)
	s.sp = sp0
	s.emit(bytecode.OpRETURN, uint32(dst))

	if err := s.finish(); err != nil {
		c.fail(pos, "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) emitOptionalDefaults(s *scope) {
	var optionals []frontend.Param
	for _, param := range s.params {
		if param.Kind == frontend.ParamOptional {
			optionals = append(optionals, param)
		}
	}
	if len(optionals) == 0 {
		return
	}

	initLabels := make([]label, len(optionals))
	for i := range optionals {
		initLabels[i] = s.asm.newLabel()
		s.asm.jmp(initLabels[i])
	}
	end := s.asm.newLabel()
	s.asm.jmp(end)

	sp0 := s.sp
	for i, param := range optionals {
		if param.Default == nil {
			c.failExpr(nil, "compiler: optional parameter %q has no default", param.Name)
			return
		}
		s.asm.bind(initLabels[i])
		value := s.alloc(1)
		c.compileExpr(s, param.Default, value)
		slot, ok := s.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown parameter %q", param.Name)
			return
		}
		s.emitMove(slot, value)
		s.sp = sp0
	}
	s.asm.bind(end)
}

func (c *Compiler) emitBlockParamCopy(s *scope) {
	if s.blockSlot < 0 {
		return
	}
	for _, param := range s.params {
		if param.Kind != frontend.ParamBlock || param.Name == "" {
			continue
		}
		slot, ok := s.locals[param.Name]
		if !ok || slot == s.blockSlot {
			continue
		}
		s.emitMove(slot, s.blockSlot)
	}
}

func (c *Compiler) emitParamDestructure(s *scope) {
	for _, param := range s.params {
		if param.Kind != frontend.ParamDestructure || param.Destructure == nil {
			continue
		}
		slot, ok := s.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown destructured parameter")
			return
		}
		sp0 := s.sp
		if !c.assignNested(s, *param.Destructure, slot) {
			return
		}
		s.sp = sp0
	}
}

func (c *Compiler) emitKeywordInit(s *scope) {
	var required []frontend.Param
	var optional []frontend.Param
	hasKeywordRest := false
	for _, param := range s.params {
		switch param.Kind {
		case frontend.ParamKeyword:
			required = append(required, param)
		case frontend.ParamOptionalKeyword:
			optional = append(optional, param)
		case frontend.ParamKeywordRest:
			hasKeywordRest = true
		}
	}
	if len(required) == 0 && len(optional) == 0 {
		return
	}

	for _, param := range required {
		slot, ok := s.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown keyword parameter %q", param.Name)
			return
		}
		s.emit(bytecode.OpKARG, uint32(slot), s.sym(param.Name))
	}

	sp0 := s.sp
	for _, param := range optional {
		slot, ok := s.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown keyword parameter %q", param.Name)
			return
		}
		have := s.asm.newLabel()
		done := s.asm.newLabel()
		s.emit(bytecode.OpKEY_P, uint32(slot), s.sym(param.Name))
		s.asm.jmpIf(bytecode.OpJMPIF, slot, have)
		value := s.alloc(1)
		c.compileExpr(s, param.Default, value)
		s.emitMove(slot, value)
		s.sp = sp0
		s.asm.jmp(done)
		s.asm.bind(have)
		s.emit(bytecode.OpKARG, uint32(slot), s.sym(param.Name))
		s.asm.bind(done)
	}

	if !hasKeywordRest {
		s.emit(bytecode.OpKEYEND)
	}
}
