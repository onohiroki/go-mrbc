package compiler

import (
	"math"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

func (c *Compiler) compileExpr(s *scope, expr frontend.Expr, dst int) {
	if c.err != nil {
		return
	}
	if expr == nil {
		s.emit(bytecode.OpLOADNIL, uint32(dst))
		return
	}
	sp0 := s.sp
	defer func() { s.sp = sp0 }()

	switch e := expr.(type) {
	case *frontend.NilLiteral:
		s.emit(bytecode.OpLOADNIL, uint32(dst))
	case *frontend.TrueLiteral:
		s.emit(bytecode.OpLOADTRUE, uint32(dst))
	case *frontend.FalseLiteral:
		s.emit(bytecode.OpLOADFALSE, uint32(dst))
	case *frontend.SelfExpr:
		s.emit(bytecode.OpLOADSELF, uint32(dst))
	case *frontend.IntegerLiteral:
		c.compileInteger(s, dst, e.Value)
	case *frontend.FloatLiteral:
		index := s.irep.PoolIndex(bytecode.FloatEntry(e.Value))
		s.emit(bytecode.OpLOADL, uint32(dst), uint32(index))
	case *frontend.StringLiteral:
		index := s.irep.PoolIndex(bytecode.StringEntry(e.Value))
		s.emit(bytecode.OpSTRING, uint32(dst), uint32(index))
	case *frontend.SymbolLiteral:
		s.emit(bytecode.OpLOADSYM, uint32(dst), s.sym(e.Value))

	case *frontend.LocalRead:
		if e.Depth == 0 {
			slot, ok := s.locals[e.Name]
			if !ok {
				c.failExpr(e, "unknown local variable %q", e.Name)
				return
			}
			s.emitMove(dst, slot)
		} else {
			slot, level, ok := s.resolveUpvar(e.Name, e.Depth)
			if !ok {
				c.failExpr(e, "cannot resolve captured local variable %q", e.Name)
				return
			}
			s.emit(bytecode.OpGETUPVAR, uint32(dst), uint32(slot), uint32(level))
		}
	case *frontend.LocalWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		if e.Depth == 0 {
			slot, ok := s.locals[e.Name]
			if !ok {
				c.failExpr(e, "unknown local variable %q", e.Name)
				return
			}
			s.emitMove(slot, value)
		} else {
			slot, level, ok := s.resolveUpvar(e.Name, e.Depth)
			if !ok {
				c.failExpr(e, "cannot resolve captured local variable %q", e.Name)
				return
			}
			s.emit(bytecode.OpSETUPVAR, uint32(value), uint32(slot), uint32(level))
		}
		s.emitMove(dst, value)

	case *frontend.GlobalRead:
		s.emit(bytecode.OpGETGV, uint32(dst), s.sym(e.Name))
	case *frontend.GlobalWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		s.emit(bytecode.OpSETGV, uint32(value), s.sym(e.Name))
		s.emitMove(dst, value)

	case *frontend.ConstantRead:
		s.emit(bytecode.OpGETCONST, uint32(dst), s.sym(e.Name))

	case *frontend.InterpolatedString:
		c.compileInterpolatedString(s, e, dst)
	case *frontend.ArrayLiteral:
		c.compileArray(s, e, dst)
	case *frontend.HashLiteral:
		c.compileHash(s, e, dst)
	case *frontend.RangeLiteral:
		base := s.alloc(2)
		c.compileExpr(s, e.Low, base)
		c.compileExpr(s, e.High, base+1)
		op := bytecode.OpRANGE_INC
		if e.Exclusive {
			op = bytecode.OpRANGE_EXC
		}
		s.emit(op, uint32(base))
		s.emitMove(dst, base)

	case *frontend.Call:
		c.compileCall(s, e, dst)
	case *frontend.IfExpr:
		c.compileIf(s, e, dst)
	case *frontend.WhileExpr:
		c.compileWhile(s, e, dst)
	case *frontend.CaseExpr:
		c.compileCase(s, e, dst)
	case *frontend.DefExpr:
		c.compileDef(s, e, dst)

	case *frontend.AndExpr:
		end := s.asm.newLabel()
		c.compileExpr(s, e.Left, dst)
		s.asm.jmpIf(bytecode.OpJMPNOT, dst, end)
		c.compileExpr(s, e.Right, dst)
		s.asm.bind(end)
	case *frontend.OrExpr:
		end := s.asm.newLabel()
		c.compileExpr(s, e.Left, dst)
		s.asm.jmpIf(bytecode.OpJMPIF, dst, end)
		c.compileExpr(s, e.Right, dst)
		s.asm.bind(end)
	case *frontend.NotExpr:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		s.emit(bytecode.OpSEND, uint32(value), s.sym("!"), 0)
		s.emitMove(dst, value)

	case *frontend.ReturnExpr:
		if e.Value == nil {
			if s.kind == scopeBlock {
				value := s.alloc(1)
				s.emit(bytecode.OpLOADNIL, uint32(value))
				s.emit(bytecode.OpRETURN_BLK, uint32(value))
			} else {
				s.emit(bytecode.OpRETNIL)
			}
			return
		}
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		if s.kind == scopeBlock {
			s.emit(bytecode.OpRETURN_BLK, uint32(value))
		} else {
			s.emit(bytecode.OpRETURN, uint32(value))
		}

	case *frontend.BreakExpr:
		if s.loop == nil {
			c.failExpr(e, "break outside a loop is not supported yet")
			return
		}
		if e.Value != nil {
			c.compileExpr(s, e.Value, s.loop.resultReg)
		} else {
			s.emit(bytecode.OpLOADNIL, uint32(s.loop.resultReg))
		}
		s.asm.jmp(s.loop.breakTarget)
	case *frontend.NextExpr:
		if s.loop == nil {
			c.failExpr(e, "next outside a loop is not supported yet")
			return
		}
		if e.Value != nil {
			tmp := s.alloc(1)
			c.compileExpr(s, e.Value, tmp)
		}
		s.asm.jmp(s.loop.continueTarget)

	case *frontend.YieldExpr:
		c.compileYield(s, e, dst)
	case *frontend.Statements:
		c.compileBody(s, e.Body, dst)
	default:
		c.failExpr(expr, "compiler: unsupported IR node %T", expr)
	}
}

func (c *Compiler) compileInteger(s *scope, dst int, value int64) {
	switch {
	case value == -1:
		s.emit(bytecode.OpLOADI__1, uint32(dst))
	case value >= 0 && value <= 7:
		s.emit(bytecode.OpLOADI_0+bytecode.Opcode(value), uint32(dst))
	case value >= 8 && value <= 255:
		s.emit(bytecode.OpLOADI8, uint32(dst), uint32(value))
	case value <= -2 && value >= -255:
		s.emit(bytecode.OpLOADINEG, uint32(dst), uint32(-value))
	case value >= math.MinInt16 && value <= math.MaxInt16:
		s.emit(bytecode.OpLOADI16, uint32(dst), uint32(uint16(int16(value))))
	case value >= math.MinInt32 && value <= math.MaxInt32:
		u := uint32(int32(value))
		s.emit(bytecode.OpLOADI32, uint32(dst), u>>16, u&0xffff)
	default:
		index := s.irep.PoolIndex(bytecode.IntEntry(value))
		s.emit(bytecode.OpLOADL, uint32(dst), uint32(index))
	}
}

func (c *Compiler) compileInterpolatedString(s *scope, e *frontend.InterpolatedString, dst int) {
	base := s.alloc(1)
	s.emit(bytecode.OpSTRING, uint32(base), uint32(s.irep.PoolIndex(bytecode.StringEntry(""))))
	for _, part := range e.Parts {
		value := s.alloc(1)
		c.compileExpr(s, part, value)
		s.emit(bytecode.OpSTRCAT, uint32(base))
		s.sp = value
	}
	s.emitMove(dst, base)
}

func (c *Compiler) compileArray(s *scope, e *frontend.ArrayLiteral, dst int) {
	if len(e.Elements) == 0 {
		base := s.alloc(1)
		s.emit(bytecode.OpARRAY, uint32(base), 0)
		s.emitMove(dst, base)
		return
	}
	base := s.alloc(len(e.Elements))
	for i, element := range e.Elements {
		c.compileExpr(s, element, base+i)
	}
	s.emit(bytecode.OpARRAY, uint32(base), uint32(len(e.Elements)))
	s.emitMove(dst, base)
}

func (c *Compiler) compileHash(s *scope, e *frontend.HashLiteral, dst int) {
	if len(e.Pairs) == 0 {
		base := s.alloc(1)
		s.emit(bytecode.OpHASH, uint32(base), 0)
		s.emitMove(dst, base)
		return
	}
	base := s.alloc(len(e.Pairs) * 2)
	for i, pair := range e.Pairs {
		c.compileExpr(s, pair.Key, base+i*2)
		c.compileExpr(s, pair.Value, base+i*2+1)
	}
	s.emit(bytecode.OpHASH, uint32(base), uint32(len(e.Pairs)))
	s.emitMove(dst, base)
}

func (c *Compiler) compileIf(s *scope, e *frontend.IfExpr, dst int) {
	cond := s.alloc(1)
	c.compileExpr(s, e.Cond, cond)
	elseLabel := s.asm.newLabel()
	endLabel := s.asm.newLabel()
	s.asm.jmpIf(bytecode.OpJMPNOT, cond, elseLabel)
	c.compileExpr(s, e.Then, dst)
	s.asm.jmp(endLabel)
	s.asm.bind(elseLabel)
	if e.Else != nil {
		c.compileExpr(s, e.Else, dst)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(dst))
	}
	s.asm.bind(endLabel)
}

func (c *Compiler) compileWhile(s *scope, e *frontend.WhileExpr, dst int) {
	start := s.asm.newLabel()
	end := s.asm.newLabel()

	s.emit(bytecode.OpLOADNIL, uint32(dst))
	s.asm.bind(start)
	cond := s.alloc(1)
	c.compileExpr(s, e.Cond, cond)
	s.asm.jmpIf(bytecode.OpJMPNOT, cond, end)

	info := &loopInfo{breakTarget: end, continueTarget: start, resultReg: dst}
	previous := s.loop
	s.loop = info
	body := s.alloc(1)
	c.compileBody(s, []frontend.Expr{e.Body}, body)
	s.loop = previous

	s.asm.jmp(start)
	s.asm.bind(end)
}

func (c *Compiler) compileCase(s *scope, e *frontend.CaseExpr, dst int) {
	subject := -1
	if e.Subject != nil {
		subject = s.alloc(1)
		c.compileExpr(s, e.Subject, subject)
	}

	end := s.asm.newLabel()
	for _, when := range e.Whens {
		match := s.asm.newLabel()
		for _, cond := range when.Conds {
			if subject >= 0 {
				pair := s.alloc(2)
				c.compileExpr(s, cond, pair)
				s.emitMove(pair+1, subject)
				s.emit(bytecode.OpSEND, uint32(pair), s.sym("==="), 1)
				s.asm.jmpIf(bytecode.OpJMPIF, pair, match)
				s.sp = pair
			} else {
				reg := s.alloc(1)
				c.compileExpr(s, cond, reg)
				s.asm.jmpIf(bytecode.OpJMPIF, reg, match)
				s.sp = reg
			}
		}
		next := s.asm.newLabel()
		s.asm.jmp(next)
		s.asm.bind(match)
		c.compileBody(s, when.Body, dst)
		s.asm.jmp(end)
		s.asm.bind(next)
	}

	if e.Else != nil {
		c.compileExpr(s, e.Else, dst)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(dst))
	}
	s.asm.bind(end)
}

func (c *Compiler) compileDef(s *scope, e *frontend.DefExpr, dst int) {
	child := c.compileMethod(s, e)
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	base := s.alloc(1)
	s.emit(bytecode.OpTDEF, uint32(base), s.sym(e.Name), uint32(index))
	s.emitMove(dst, base)
}

func (c *Compiler) compileCall(s *scope, e *frontend.Call, dst int) {
	const maxArgs = 14
	if len(e.Args) > maxArgs {
		c.failExpr(e, "too many arguments (maximum %d)", maxArgs)
		return
	}

	blockIndex := -1
	if e.Block != nil {
		child := c.compileBlock(s, e.Block)
		if child == nil {
			return
		}
		blockIndex = s.irep.AddChild(child)
	}

	extra := 0
	if blockIndex >= 0 {
		extra = 1
	}
	n := len(e.Args)
	if e.ImplicitSelf() {
		base := s.alloc(1 + n + extra)
		for i, arg := range e.Args {
			c.compileExpr(s, arg, base+1+i)
		}
		if blockIndex >= 0 {
			s.emit(bytecode.OpBLOCK, uint32(base+1+n), uint32(blockIndex))
			s.emit(bytecode.OpSSENDB, uint32(base), s.sym(e.Name), uint32(n))
		} else {
			s.emit(bytecode.OpSSEND, uint32(base), s.sym(e.Name), uint32(n))
		}
		s.emitMove(dst, base)
		return
	}

	base := s.alloc(1 + n + extra)
	c.compileExpr(s, e.Receiver, base)
	for i, arg := range e.Args {
		c.compileExpr(s, arg, base+1+i)
	}
	if blockIndex >= 0 {
		s.emit(bytecode.OpBLOCK, uint32(base+1+n), uint32(blockIndex))
		s.emit(bytecode.OpSENDB, uint32(base), s.sym(e.Name), uint32(n))
	} else {
		s.emit(bytecode.OpSEND, uint32(base), s.sym(e.Name), uint32(n))
	}
	s.emitMove(dst, base)
}

func (c *Compiler) compileYield(s *scope, e *frontend.YieldExpr, dst int) {
	if s.method == nil || s.method.methodInfo == nil {
		c.failExpr(e, "yield outside a method is not supported")
		return
	}
	if s.yieldLevel != 0 {
		c.failExpr(e, "yield inside a block is not supported yet")
		return
	}
	if len(e.Args) > 14 {
		c.failExpr(e, "too many yield arguments")
		return
	}
	base := s.alloc(1 + len(e.Args))
	for i, arg := range e.Args {
		c.compileExpr(s, arg, base+1+i)
	}
	info := s.method.methodInfo
	aspec := uint32(info.ainfo<<4) & 0xffff
	s.emit(bytecode.OpBLKPUSH, uint32(base), aspec)
	s.emit(bytecode.OpBLKCALL, uint32(base), uint32(len(e.Args)))
	s.emitMove(dst, base)
}
