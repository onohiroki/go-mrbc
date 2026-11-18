package compiler

import (
	"fmt"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

// Compiler lowers the internal IR to mruby 4.0.0 bytecode.
type Compiler struct {
	source *frontend.Source
	err    error
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
		tmp := s.alloc(1)
		c.compileExpr(s, expr, tmp)
		s.sp = sp0
	}
}

func (c *Compiler) compileMethod(parent *scope, def *frontend.DefExpr) *bytecode.Irep {
	s := c.newScope(parent, scopeMethod, def.Params, def.Locals)
	if c.err != nil {
		return nil
	}
	aspec, ainfo := s.enterAspec()
	s.methodInfo = &methodInfo{aspec: aspec, ainfo: ainfo}
	s.emit(bytecode.OpENTER, aspec)
	c.emitOptionalDefaults(s)

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
	aspec, _ := s.enterAspec()
	s.emit(bytecode.OpENTER, aspec)
	c.emitOptionalDefaults(s)

	sp0 := s.sp
	if len(block.Body) == 0 {
		s.emit(bytecode.OpRETNIL)
	} else {
		dst := s.alloc(1)
		c.compileBody(s, block.Body, dst)
		s.emit(bytecode.OpRETURN, uint32(dst))
		s.sp = sp0
	}

	if err := s.finish(); err != nil {
		c.fail(block.Pos, "%v", err)
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
