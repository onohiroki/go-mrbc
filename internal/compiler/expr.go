package compiler

import (
	"fmt"
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
	s.markLine(expr.Position())
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
	case *frontend.ObjectClassExpr:
		s.emit(bytecode.OpOCLASS, uint32(dst))
	case *frontend.IntegerLiteral:
		c.compileInteger(s, dst, e.Value)
	case *frontend.BigIntLiteral:
		s.emit(bytecode.OpLOADL, uint32(dst), uint32(s.irep.PoolIndex(bytecode.BigIntEntry(e.Digits, e.Negative))))
	case *frontend.FloatLiteral:
		index := s.irep.PoolIndex(bytecode.FloatEntry(e.Value))
		s.emit(bytecode.OpLOADL, uint32(dst), uint32(index))
	case *frontend.StringLiteral:
		index := s.irep.PoolIndex(bytecode.StringEntry(e.Value))
		s.emit(bytecode.OpSTRING, uint32(dst), uint32(index))
	case *frontend.SymbolLiteral:
		s.emit(bytecode.OpLOADSYM, uint32(dst), s.sym(e.Value))

	case *frontend.LocalRead:
		depth := s.effectiveDepth(e.Depth)
		if depth == 0 {
			slot, ok := s.locals[e.Name]
			if !ok {
				c.failExpr(e, "unknown local variable %q", e.Name)
				return
			}
			s.emitMove(dst, slot)
		} else {
			slot, level, ok := s.resolveUpvar(e.Name, depth)
			if !ok {
				c.failExpr(e, "cannot resolve captured local variable %q", e.Name)
				return
			}
			s.emit(bytecode.OpGETUPVAR, uint32(dst), uint32(slot), uint32(level))
		}
	case *frontend.LocalWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		depth := s.effectiveDepth(e.Depth)
		if depth == 0 {
			slot, ok := s.locals[e.Name]
			if !ok {
				c.failExpr(e, "unknown local variable %q", e.Name)
				return
			}
			s.emitMove(slot, value)
		} else {
			slot, level, ok := s.resolveUpvar(e.Name, depth)
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
	case *frontend.SymbolInterpolation:
		base := s.alloc(1)
		c.compileStringParts(s, e.Parts, base)
		s.emit(bytecode.OpINTERN, uint32(base))
		s.emitMove(dst, base)
	case *frontend.RegexLiteral:
		base := s.alloc(3)
		s.emit(bytecode.OpOCLASS, uint32(base))
		s.emit(bytecode.OpGETMCNST, uint32(base), s.sym("Regexp"))
		s.emit(bytecode.OpSTRING, uint32(base+1), uint32(s.irep.PoolIndex(bytecode.StringEntry(e.Source))))
		s.emit(bytecode.OpSTRING, uint32(base+2), uint32(s.irep.PoolIndex(bytecode.StringEntry(e.Flags))))
		s.emit(bytecode.OpSEND, uint32(base), s.sym("compile"), 2)
		s.emitMove(dst, base)
	case *frontend.RegexInterpolation:
		base := s.alloc(3)
		s.emit(bytecode.OpOCLASS, uint32(base))
		s.emit(bytecode.OpGETMCNST, uint32(base), s.sym("Regexp"))
		c.compileStringParts(s, e.Parts, base+1)
		s.emit(bytecode.OpSTRING, uint32(base+2), uint32(s.irep.PoolIndex(bytecode.StringEntry(e.Flags))))
		s.emit(bytecode.OpSEND, uint32(base), s.sym("compile"), 2)
		s.emitMove(dst, base)
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

	case *frontend.InstanceRead:
		s.emit(bytecode.OpGETIV, uint32(dst), s.sym(e.Name))
	case *frontend.BlockSlotExpr:
		if s.method == nil || s.method.methodInfo == nil || s.method.methodInfo.blockSlot < 0 || s.yieldLevel < 0 {
			c.failExpr(e, "block argument forwarding is not available here")
			return
		}
		slot := s.method.methodInfo.blockSlot
		if s.yieldLevel == 0 {
			s.emitMove(dst, slot)
		} else {
			s.emit(bytecode.OpGETUPVAR, uint32(dst), uint32(slot), uint32(s.yieldLevel-1))
		}

	case *frontend.InstanceWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		s.emit(bytecode.OpSETIV, uint32(value), s.sym(e.Name))
		s.emitMove(dst, value)
	case *frontend.ClassVariableRead:
		s.emit(bytecode.OpGETCV, uint32(dst), s.sym(e.Name))
	case *frontend.ClassVariableWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		s.emit(bytecode.OpSETCV, uint32(value), s.sym(e.Name))
		s.emitMove(dst, value)
	case *frontend.ConstantWrite:
		value := s.alloc(1)
		c.compileExpr(s, e.Value, value)
		s.emit(bytecode.OpSETCONST, uint32(value), s.sym(e.Name))
		s.emitMove(dst, value)
	case *frontend.ConstantPathRead:
		c.compileExpr(s, e.Parent, dst)
		s.emit(bytecode.OpGETMCNST, uint32(dst), s.sym(e.Name))
	case *frontend.ConstantPathWrite:
		base := s.alloc(2)
		if e.Parent != nil {
			c.compileExpr(s, e.Parent, base+1)
		} else {
			s.emit(bytecode.OpOCLASS, uint32(base+1))
		}
		c.compileExpr(s, e.Value, base)
		s.emit(bytecode.OpSETMCNST, uint32(base), s.sym(e.Name))
		s.emitMove(dst, base)

	case *frontend.ClassDef:
		c.compileClass(s, e, dst)
	case *frontend.ModuleDef:
		c.compileModule(s, e, dst)
	case *frontend.SingletonClassDef:
		c.compileSingletonClass(s, e, dst)

	case *frontend.Call:
		c.compileCall(s, e, dst)
	case *frontend.MatchWriteExpr:
		c.compileMatchWrite(s, e, dst)
	case *frontend.SuperExpr:
		c.compileSuper(s, e, dst)
	case *frontend.LambdaExpr:
		c.compileLambdaExpr(s, e, dst)
	case *frontend.MultiWrite:
		c.compileMultiWrite(s, e, dst)
	case *frontend.IfExpr:
		c.compileIf(s, e, dst)
	case *frontend.WhileExpr:
		c.compileWhile(s, e, dst)
	case *frontend.CaseExpr:
		c.compileCase(s, e, dst)
	case *frontend.CaseMatchExpr:
		c.compileCaseMatch(s, e, dst)
	case *frontend.ForExpr:
		c.compileFor(s, e, dst)
	case *frontend.BeginExpr:
		c.compileBegin(s, e, dst)
	case *frontend.RetryExpr:
		c.compileRetry(s, e)
	case *frontend.RedoExpr:
		c.compileRedo(s, e)
	case *frontend.AliasExpr:
		s.emit(bytecode.OpALIAS, s.sym(e.NewName), s.sym(e.OldName))
		s.emit(bytecode.OpLOADNIL, uint32(dst))
	case *frontend.UndefExpr:
		for _, name := range e.Names {
			s.emit(bytecode.OpUNDEF, s.sym(name))
		}
		s.emit(bytecode.OpLOADNIL, uint32(dst))
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
		if s.loop != nil {
			if e.Value != nil {
				c.compileExpr(s, e.Value, s.loop.resultReg)
			} else {
				s.emit(bytecode.OpLOADNIL, uint32(s.loop.resultReg))
			}
			s.asm.jmpuw(s.loop.breakTarget)
			return
		}
		if s.kind == scopeBlock {
			value := s.alloc(1)
			if e.Value != nil {
				c.compileExpr(s, e.Value, value)
			} else {
				s.emit(bytecode.OpLOADNIL, uint32(value))
			}
			s.emit(bytecode.OpBREAK, uint32(value))
			return
		}
		if s.kind == scopeLambda {
			if e.Value == nil {
				s.emit(bytecode.OpRETNIL)
				return
			}
			value := s.alloc(1)
			c.compileExpr(s, e.Value, value)
			s.emit(bytecode.OpRETURN, uint32(value))
			return
		}
		c.failExpr(e, "break outside a loop is not supported yet")
	case *frontend.NextExpr:
		if s.loop != nil {
			if e.Value != nil {
				tmp := s.alloc(1)
				c.compileExpr(s, e.Value, tmp)
			}
			s.asm.jmpuw(s.loop.continueTarget)
			return
		}
		if s.kind == scopeBlock || s.kind == scopeLambda {
			if e.Value == nil {
				s.emit(bytecode.OpRETNIL)
				return
			}
			value := s.alloc(1)
			c.compileExpr(s, e.Value, value)
			s.emit(bytecode.OpRETURN, uint32(value))
			return
		}
		c.failExpr(e, "next outside a loop is not supported yet")

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
	c.compileStringParts(s, e.Parts, base)
	s.emitMove(dst, base)
}

func (c *Compiler) compileStringParts(s *scope, parts []frontend.Expr, base int) {
	s.emit(bytecode.OpSTRING, uint32(base), uint32(s.irep.PoolIndex(bytecode.StringEntry(""))))
	headerSP := s.sp
	for _, part := range parts {
		s.setSP(base + 2)
		c.compileExpr(s, part, base+1)
		s.emit(bytecode.OpSTRCAT, uint32(base))
	}
	s.sp = headerSP
}

func (c *Compiler) compileArray(s *scope, e *frontend.ArrayLiteral, dst int) {
	hasSplat := false
	for _, element := range e.Elements {
		if _, ok := element.(*frontend.SplatArg); ok {
			hasSplat = true
			break
		}
	}
	if hasSplat {
		c.buildArgArray(s, e.Elements, dst)
		return
	}
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
	hasSplat := false
	for _, pair := range e.Pairs {
		if pair.Key == nil {
			hasSplat = true
			break
		}
	}
	if hasSplat {
		base := s.alloc(1)
		s.emit(bytecode.OpHASH, uint32(base), 0)
		headerSP := s.sp
		for _, pair := range e.Pairs {
			if pair.Key == nil {
				s.setSP(base + 2)
				c.compileExpr(s, pair.Value, base+1)
				s.emit(bytecode.OpHASHCAT, uint32(base))
				continue
			}
			s.setSP(base + 3)
			c.compileExpr(s, pair.Key, base+1)
			c.compileExpr(s, pair.Value, base+2)
			s.emit(bytecode.OpHASHADD, uint32(base), 1)
		}
		s.sp = headerSP
		s.emitMove(dst, base)
		return
	}
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
	bodyStart := s.asm.newLabel()
	s.asm.bind(bodyStart)
	prevRedo, prevHasRedo := s.redoTarget, s.hasRedo
	s.redoTarget, s.hasRedo = bodyStart, true
	body := s.alloc(1)
	c.compileBody(s, []frontend.Expr{e.Body}, body)
	s.redoTarget, s.hasRedo = prevRedo, prevHasRedo
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

func (c *Compiler) compileCaseMatch(s *scope, e *frontend.CaseMatchExpr, dst int) {
	sp0 := s.sp
	subject := s.alloc(1)
	c.compileExpr(s, e.Subject, subject)
	spAfterSubject := s.sp

	end := s.asm.newLabel()
	for _, in := range e.Ins {
		next := s.asm.newLabel()
		s.sp = spAfterSubject
		if !c.compilePattern(s, in.Pattern, subject, next) {
			return
		}
		if in.Guard != nil {
			guard := s.alloc(1)
			c.compileExpr(s, in.Guard, guard)
			s.asm.jmpIf(bytecode.OpJMPNOT, guard, next)
		}
		s.sp = spAfterSubject
		c.compileBody(s, in.Body, dst)
		s.asm.jmp(end)
		s.asm.bind(next)
		s.sp = spAfterSubject
	}

	if e.HasElse {
		c.compileBody(s, e.Else, dst)
	} else {
		s.emit(bytecode.OpLOADFALSE, uint32(dst))
		s.emit(bytecode.OpMATCHERR, uint32(dst))
	}
	s.asm.bind(end)
	s.sp = sp0
}

func (c *Compiler) compilePattern(s *scope, pat frontend.Pattern, subject int, fail label) bool {
	switch p := pat.(type) {
	case *frontend.ValuePattern:
		base := s.alloc(2)
		c.compileExpr(s, p.Value, base)
		s.emitMove(base+1, subject)
		s.emit(bytecode.OpSEND, uint32(base), s.sym("==="), 1)
		s.asm.jmpIf(bytecode.OpJMPNOT, base, fail)
		return true
	case *frontend.EqualPattern:
		base := s.alloc(2)
		s.emitMove(base, subject)
		c.compileExpr(s, p.Value, base+1)
		s.emit(bytecode.OpSEND, uint32(base), s.sym("=="), 1)
		s.asm.jmpIf(bytecode.OpJMPNOT, base, fail)
		return true
	case *frontend.BindPattern:
		return c.assignDirect(s, p.Target, subject)
	case *frontend.CapturePattern:
		if !c.compilePattern(s, p.Value, subject, fail) {
			return false
		}
		return c.assignDirect(s, p.Target, subject)
	case *frontend.ArrayPattern:
		if p.Constant != nil {
			check := &frontend.ValuePattern{Value: p.Constant}
			if !c.compilePattern(s, check, subject, fail) {
				return false
			}
		}
		base := s.sp
		deconstructed := s.alloc(1)
		s.emitMove(deconstructed, subject)
		s.emit(bytecode.OpSEND, uint32(deconstructed), s.sym("deconstruct"), 0)
		size := s.alloc(1)
		s.emitMove(size, deconstructed)
		s.emit(bytecode.OpSEND, uint32(size), s.sym("size"), 0)
		want := s.alloc(1)
		c.compileInteger(s, want, int64(len(p.Elements)))
		s.emit(bytecode.OpSEND, uint32(size), s.sym("=="), 1)
		s.asm.jmpIf(bytecode.OpJMPNOT, size, fail)

		elementBase := s.sp
		for i, elementPattern := range p.Elements {
			s.sp = elementBase
			element := s.alloc(1)
			s.emitMove(element, deconstructed)
			index := s.alloc(1)
			c.compileInteger(s, index, int64(i))
			s.emit(bytecode.OpSEND, uint32(element), s.sym("[]"), 1)
			if !c.compilePattern(s, elementPattern, element, fail) {
				return false
			}
		}
		s.sp = base
		return true
	case *frontend.HashPattern:
		return c.compileHashPattern(s, p, subject, fail)
	default:
		c.failExpr(nil, "compiler: unsupported pattern %T", pat)
		return false
	}
}

func (c *Compiler) compileHashPattern(s *scope, p *frontend.HashPattern, subject int, fail label) bool {
	if p.Constant != nil {
		check := &frontend.ValuePattern{Value: p.Constant}
		if !c.compilePattern(s, check, subject, fail) {
			return false
		}
	}
	sp0 := s.sp
	count := len(p.Entries)
	keys := s.alloc(count)
	if count == 0 {
		keys = s.alloc(1)
		s.emit(bytecode.OpARRAY, uint32(keys), 0)
	} else {
		for i, entry := range p.Entries {
			s.emit(bytecode.OpLOADSYM, uint32(keys+i), s.sym(entry.Key))
		}
		s.emit(bytecode.OpARRAY, uint32(keys), uint32(count))
	}

	deconstructed := s.alloc(2)
	s.emitMove(deconstructed, subject)
	s.emitMove(deconstructed+1, keys)
	s.emit(bytecode.OpSEND, uint32(deconstructed), s.sym("deconstruct_keys"), 1)

	values := s.alloc(1)
	s.emitMove(values, deconstructed)
	s.setSP(values + 2)
	s.emitMove(values+1, keys)
	s.emit(bytecode.OpSEND, uint32(values), s.sym("__pat_values"), 1)
	s.asm.jmpIf(bytecode.OpJMPNOT, values, fail)

	elementBase := values + 1
	for i, entry := range p.Entries {
		s.setSP(elementBase)
		value := s.alloc(1)
		s.emitMove(value, values)
		index := s.alloc(1)
		c.compileInteger(s, index, int64(i))
		s.emit(bytecode.OpSEND, uint32(value), s.sym("[]"), 1)
		if !c.compilePattern(s, entry.Value, value, fail) {
			return false
		}
	}

	if p.Rest != nil {
		s.setSP(elementBase)
		rest := s.alloc(2)
		s.emitMove(rest, deconstructed)
		s.emitMove(rest+1, keys)
		s.emit(bytecode.OpSEND, uint32(rest), s.sym("__except"), 1)
		if !c.assignDirect(s, *p.Rest, rest) {
			return false
		}
	}

	s.sp = sp0
	return true
}

func (c *Compiler) compileDef(s *scope, e *frontend.DefExpr, dst int) {
	child := c.compileMethod(s, e)
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	base := s.alloc(1)
	if e.Receiver == nil {
		s.emit(bytecode.OpTDEF, uint32(base), s.sym(e.Name), uint32(index))
	} else {
		c.compileExpr(s, e.Receiver, base)
		s.emit(bytecode.OpSDEF, uint32(base), s.sym(e.Name), uint32(index))
	}
	s.emitMove(dst, base)
}

func (c *Compiler) compileClass(s *scope, e *frontend.ClassDef, dst int) {
	base := s.alloc(2)
	if e.Outer != nil {
		c.compileExpr(s, e.Outer, base)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(base))
	}
	if e.Superclass != nil {
		c.compileExpr(s, e.Superclass, base+1)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(base+1))
	}
	s.emit(bytecode.OpCLASS, uint32(base), s.sym(e.Name))
	child := c.compileClassBody(s, e.Locals, e.Body, e.Position())
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	s.emit(bytecode.OpEXEC, uint32(base), uint32(index))
	s.emitMove(dst, base)
}

func (c *Compiler) compileModule(s *scope, e *frontend.ModuleDef, dst int) {
	base := s.alloc(1)
	if e.Outer != nil {
		c.compileExpr(s, e.Outer, base)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(base))
	}
	s.emit(bytecode.OpMODULE, uint32(base), s.sym(e.Name))
	child := c.compileClassBody(s, e.Locals, e.Body, e.Position())
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	s.emit(bytecode.OpEXEC, uint32(base), uint32(index))
	s.emitMove(dst, base)
}

func (c *Compiler) compileSingletonClass(s *scope, e *frontend.SingletonClassDef, dst int) {
	base := s.alloc(1)
	c.compileExpr(s, e.Expression, base)
	s.emit(bytecode.OpSCLASS, uint32(base))
	child := c.compileClassBody(s, e.Locals, e.Body, e.Position())
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	s.emit(bytecode.OpEXEC, uint32(base), uint32(index))
	s.emitMove(dst, base)
}

func (c *Compiler) compileLambdaExpr(s *scope, e *frontend.LambdaExpr, dst int) {
	child := c.compileLambda(s, e)
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)
	base := s.alloc(1)
	s.emit(bytecode.OpLAMBDA, uint32(base), uint32(index))
	s.emitMove(dst, base)
}

func (c *Compiler) compileMultiWrite(s *scope, e *frontend.MultiWrite, dst int) {
	if len(e.Targets) > 255 || len(e.Posts) > 255 {
		c.failExpr(e, "too many multiple assignment targets")
		return
	}
	value := s.alloc(1)
	c.compileExpr(s, e.Value, value)
	sp0 := s.sp
	if !c.assignDestructure(s, e.Targets, e.Rest, e.Posts, value, sp0) {
		return
	}
	s.emitMove(dst, value)
}

func (c *Compiler) assignDestructure(s *scope, targets []frontend.MultiTarget, rest *frontend.MultiTarget, posts []frontend.MultiTarget, value, sp0 int) bool {
	if rest == nil {
		for i, target := range targets {
			if !c.assignIndexed(s, target, value, i, sp0) {
				return false
			}
		}
		return true
	}

	for i, target := range targets {
		if !c.assignIndexed(s, target, value, i, sp0) {
			return false
		}
	}
	work := s.alloc(1)
	s.emitMove(work, value)
	s.setSP(work + 1 + len(posts))
	s.emit(bytecode.OpAPOST, uint32(work), uint32(len(targets)), uint32(len(posts)))
	if !c.assignDirect(s, *rest, work) {
		return false
	}
	for i, target := range posts {
		if !c.assignDirect(s, target, work+1+i) {
			return false
		}
	}
	s.sp = sp0
	return true
}

func (c *Compiler) assignIndexed(s *scope, target frontend.MultiTarget, value, index, sp0 int) bool {
	switch target.Kind {
	case frontend.TargetLocal:
		depth := s.effectiveDepth(target.Depth)
		if depth == 0 {
			slot, ok := s.locals[target.Name]
			if !ok {
				c.fail(target.Pos, "unknown local variable %q", target.Name)
				return false
			}
			s.emit(bytecode.OpAREF, uint32(slot), uint32(value), uint32(index))
			return true
		}
		slot, level, ok := s.resolveUpvar(target.Name, depth)
		if !ok {
			c.fail(target.Pos, "cannot resolve captured local variable %q", target.Name)
			return false
		}
		temp := s.alloc(1)
		s.emit(bytecode.OpAREF, uint32(temp), uint32(value), uint32(index))
		s.emit(bytecode.OpSETUPVAR, uint32(temp), uint32(slot), uint32(level))
		s.sp = sp0
		return true
	case frontend.TargetGlobal, frontend.TargetInstance, frontend.TargetConstant:
		temp := s.alloc(1)
		s.emit(bytecode.OpAREF, uint32(temp), uint32(value), uint32(index))
		if !c.assignDirect(s, target, temp) {
			return false
		}
		s.sp = sp0
		return true
	case frontend.TargetCall, frontend.TargetIndex:
		temp := s.alloc(1)
		s.emit(bytecode.OpAREF, uint32(temp), uint32(value), uint32(index))
		ok := c.emitTargetAssign(s, target, temp)
		s.sp = sp0
		return ok
	case frontend.TargetNested:
		temp := s.alloc(1)
		s.emit(bytecode.OpAREF, uint32(temp), uint32(value), uint32(index))
		ok := c.assignNested(s, target, temp)
		s.sp = sp0
		return ok
	default:
		c.fail(target.Pos, "unsupported multiple assignment target")
		return false
	}
}

func (c *Compiler) assignDirect(s *scope, target frontend.MultiTarget, src int) bool {
	switch target.Kind {
	case frontend.TargetLocal:
		depth := s.effectiveDepth(target.Depth)
		if depth == 0 {
			slot, ok := s.locals[target.Name]
			if !ok {
				c.fail(target.Pos, "unknown local variable %q", target.Name)
				return false
			}
			s.emitMove(slot, src)
			return true
		}
		slot, level, ok := s.resolveUpvar(target.Name, depth)
		if !ok {
			c.fail(target.Pos, "cannot resolve captured local variable %q", target.Name)
			return false
		}
		s.emit(bytecode.OpSETUPVAR, uint32(src), uint32(slot), uint32(level))
		return true
	case frontend.TargetGlobal:
		s.emit(bytecode.OpSETGV, uint32(src), s.sym(target.Name))
		return true
	case frontend.TargetInstance:
		s.emit(bytecode.OpSETIV, uint32(src), s.sym(target.Name))
		return true
	case frontend.TargetConstant:
		s.emit(bytecode.OpSETCONST, uint32(src), s.sym(target.Name))
		return true
	case frontend.TargetCall, frontend.TargetIndex:
		sp0 := s.sp
		ok := c.emitTargetAssign(s, target, src)
		s.sp = sp0
		return ok
	case frontend.TargetNested:
		sp0 := s.sp
		ok := c.assignNested(s, target, src)
		s.sp = sp0
		return ok
	default:
		c.fail(target.Pos, "unsupported multiple assignment target")
		return false
	}
}

func (c *Compiler) assignNested(s *scope, target frontend.MultiTarget, src int) bool {
	sp0 := s.sp
	if !c.assignDestructure(s, target.Lefts, target.Rest, target.Posts, src, sp0) {
		return false
	}
	s.sp = sp0
	return true
}

func (c *Compiler) emitTargetAssign(s *scope, target frontend.MultiTarget, src int) bool {
	if target.Receiver == nil {
		c.fail(target.Pos, "multiple assignment target has no receiver")
		return false
	}
	switch target.Kind {
	case frontend.TargetCall:
		base := s.alloc(2)
		c.compileExpr(s, target.Receiver, base)
		s.emitMove(base+1, src)
		s.emit(bytecode.OpSEND, uint32(base), s.sym(target.Name), 1)
		return true
	case frontend.TargetIndex:
		n := len(target.Args)
		if n > 13 {
			c.fail(target.Pos, "too many index arguments in multiple assignment")
			return false
		}
		base := s.alloc(2 + n)
		c.compileExpr(s, target.Receiver, base)
		for i, arg := range target.Args {
			c.compileExpr(s, arg, base+1+i)
		}
		s.emitMove(base+1+n, src)
		s.emit(bytecode.OpSEND, uint32(base), s.sym("[]="), uint32(n+1))
		return true
	default:
		c.fail(target.Pos, "unsupported multiple assignment target")
		return false
	}
}

func (c *Compiler) compileFor(s *scope, e *frontend.ForExpr, dst int) {
	collection := s.alloc(1)
	c.compileExpr(s, e.Collection, collection)
	child := c.compileForBlock(s, e)
	if child == nil {
		return
	}
	index := s.irep.AddChild(child)

	base := s.alloc(2)
	s.emitMove(base, collection)
	s.emit(bytecode.OpBLOCK, uint32(base+1), uint32(index))
	s.emit(bytecode.OpSENDB, uint32(base), s.sym("each"), 0)
	s.emitMove(dst, base)
}

func (c *Compiler) compileForBlock(parent *scope, e *frontend.ForExpr) *bytecode.Irep {
	name := fmt.Sprintf("__gomrbc_for_%d", c.forCounter)
	c.forCounter++
	params := []frontend.Param{{Name: name, Kind: frontend.ParamRequired}}
	s := c.newScope(parent, scopeBlock, params, nil)
	if c.err != nil {
		return nil
	}
	s.markLine(e.Position())
	s.forDepth = parent.forDepth + 1

	aspec, _ := s.enterAspec()
	s.emit(bytecode.OpENTER, aspec)

	synthetic, ok := s.locals[name]
	if !ok {
		c.fail(e.Position(), "compiler: internal error: missing for-loop value slot")
		return nil
	}
	sp0 := s.sp
	if e.Rest == nil && len(e.Targets) == 1 && len(e.Posts) == 0 {
		if !c.assignDirect(s, e.Targets[0], synthetic) {
			return nil
		}
	} else if !c.assignDestructure(s, e.Targets, e.Rest, e.Posts, synthetic, sp0) {
		return nil
	}
	s.sp = sp0

	redoLabel := s.asm.newLabel()
	s.asm.bind(redoLabel)
	prevRedo, prevHasRedo := s.redoTarget, s.hasRedo
	s.redoTarget, s.hasRedo = redoLabel, true
	if len(e.Body) > 0 {
		dst := s.alloc(1)
		c.compileBody(s, e.Body, dst)
		s.sp = sp0
	}
	s.redoTarget, s.hasRedo = prevRedo, prevHasRedo
	s.emit(bytecode.OpRETNIL)

	if err := s.finish(); err != nil {
		c.fail(e.Position(), "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileBegin(s *scope, e *frontend.BeginExpr, dst int) {
	hasRescue := len(e.Rescues) > 0
	hasEnsure := len(e.Ensure) > 0
	if !hasRescue && !hasEnsure {
		c.compileBody(s, e.Body, dst)
		if len(e.Else) > 0 {
			c.compileBody(s, e.Else, dst)
		}
		return
	}

	sp0 := s.sp
	beginLabel := s.asm.newLabel()
	after := s.asm.newLabel()
	s.asm.bind(beginLabel)

	var rescueEnd, rescueTarget label
	if hasEnsure {
		s.asm.addCatch(bytecode.CatchEnsure, beginLabel, after, after)
	}
	if hasRescue {
		rescueEnd = s.asm.newLabel()
		rescueTarget = s.asm.newLabel()
		s.asm.addCatch(bytecode.CatchRescue, beginLabel, rescueEnd, rescueTarget)
	}

	if len(e.Body) == 0 {
		s.emit(bytecode.OpLOADNIL, uint32(dst))
	} else {
		c.compileBody(s, e.Body, dst)
	}

	if hasRescue {
		if len(e.Else) > 0 {
			elseLabel := s.asm.newLabel()
			s.asm.bind(rescueEnd)
			s.asm.jmp(elseLabel)
			s.asm.bind(rescueTarget)
			c.compileRescueHandlers(s, e, dst, beginLabel, after)
			s.asm.bind(elseLabel)
			c.compileBody(s, e.Else, dst)
			s.asm.jmp(after)
		} else {
			s.asm.bind(rescueEnd)
			s.asm.jmp(after)
			s.asm.bind(rescueTarget)
			c.compileRescueHandlers(s, e, dst, beginLabel, after)
		}
	}

	if hasEnsure {
		s.asm.bind(after)
		exc := s.alloc(1)
		s.emit(bytecode.OpEXCEPT, uint32(exc))
		esp := s.sp
		if len(e.Ensure) > 0 {
			tmp := s.alloc(1)
			c.compileBody(s, e.Ensure, tmp)
			s.sp = esp
		}
		s.emit(bytecode.OpRAISEIF, uint32(exc))
	} else {
		s.asm.bind(after)
	}
	s.sp = sp0
}

func (c *Compiler) compileRescueHandlers(s *scope, e *frontend.BeginExpr, dst int, beginLabel, after label) {
	exc := s.alloc(1)
	s.emit(bytecode.OpEXCEPT, uint32(exc))
	sp0 := s.sp

	for _, clause := range e.Rescues {
		bodyLabel := s.asm.newLabel()
		nextLabel := s.asm.newLabel()
		classReg := s.alloc(1)

		if len(clause.Classes) == 0 {
			s.emit(bytecode.OpGETCONST, uint32(classReg), s.sym("StandardError"))
			s.emit(bytecode.OpRESCUE, uint32(exc), uint32(classReg))
			s.asm.jmpIf(bytecode.OpJMPIF, classReg, bodyLabel)
			s.asm.jmp(nextLabel)
		} else {
			hasSplat := false
			for _, classExpr := range clause.Classes {
				if _, ok := classExpr.(*frontend.SplatArg); ok {
					hasSplat = true
					break
				}
			}
			if hasSplat {
				pair := s.alloc(2)
				c.buildArgArray(s, clause.Classes, pair)
				s.setSP(pair + 2)
				s.emitMove(pair+1, exc)
				s.emit(bytecode.OpSEND, uint32(pair), s.sym("__case_eqq"), 1)
				s.asm.jmpIf(bytecode.OpJMPIF, pair, bodyLabel)
				s.asm.jmp(nextLabel)
			} else {
				for _, classExpr := range clause.Classes {
					c.compileExpr(s, classExpr, classReg)
					s.emit(bytecode.OpRESCUE, uint32(exc), uint32(classReg))
					s.asm.jmpIf(bytecode.OpJMPIF, classReg, bodyLabel)
				}
				s.asm.jmp(nextLabel)
			}
		}

		s.asm.bind(bodyLabel)
		if clause.Target != nil {
			if !c.assignDirect(s, *clause.Target, exc) {
				return
			}
		}
		previous := s.rescue
		s.rescue = &rescueContext{retryLabel: beginLabel}
		c.compileBody(s, clause.Body, dst)
		s.rescue = previous
		s.asm.jmp(after)
		s.asm.bind(nextLabel)
		s.sp = sp0
	}

	s.emit(bytecode.OpRAISEIF, uint32(exc))
	s.asm.jmp(after)
}

func (c *Compiler) compileRetry(s *scope, e *frontend.RetryExpr) {
	if s.rescue == nil {
		c.failExpr(e, "retry outside a rescue clause is not supported")
		return
	}
	s.asm.jmpuw(s.rescue.retryLabel)
}

func (c *Compiler) compileRedo(s *scope, e *frontend.RedoExpr) {
	if !s.hasRedo {
		c.failExpr(e, "redo outside a loop or block is not supported")
		return
	}
	s.asm.jmpuw(s.redoTarget)
}

func (c *Compiler) compileMatchWrite(s *scope, e *frontend.MatchWriteExpr, dst int) {
	sp0 := s.sp
	result := s.alloc(1)
	c.compileExpr(s, e.Call, result)
	for _, target := range e.Targets {
		if target.Kind != frontend.TargetLocal {
			c.fail(target.Pos, "only local variables are supported as match-write targets")
			return
		}
		base := s.alloc(2)
		s.emit(bytecode.OpGETGV, uint32(base), s.sym("$~"))
		s.emit(bytecode.OpLOADSYM, uint32(base+1), s.sym(target.Name))
		s.emit(bytecode.OpSEND, uint32(base), s.sym("[]"), 1)
		if !c.assignDirect(s, target, base) {
			return
		}
		s.sp = sp0 + 1
	}
	s.emitMove(dst, result)
	s.sp = sp0
}

func (c *Compiler) compileCall(s *scope, e *frontend.Call, dst int) {
	if e.SafeNavigation && e.Receiver != nil {
		receiver := s.alloc(1)
		c.compileExpr(s, e.Receiver, receiver)
		nilLabel := s.asm.newLabel()
		endLabel := s.asm.newLabel()
		s.asm.jmpIf(bytecode.OpJMPNIL, receiver, nilLabel)
		clone := *e
		clone.SafeNavigation = false
		c.compileCallPrepared(s, &clone, dst, receiver)
		s.asm.jmp(endLabel)
		s.asm.bind(nilLabel)
		s.emit(bytecode.OpLOADNIL, uint32(dst))
		s.asm.bind(endLabel)
		return
	}
	c.compileCallPrepared(s, e, dst, -1)
}

func (c *Compiler) compileCallPrepared(s *scope, e *frontend.Call, dst, preReceiver int) {
	if e.Block != nil && e.BlockArg != nil {
		c.failExpr(e, "both a block and a block argument are not allowed")
		return
	}
	if e.ForwardAll {
		c.compileForwardAll(s, e, dst)
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

	hasPositionalSplat := false
	for _, arg := range e.Args {
		if _, ok := arg.(*frontend.SplatArg); ok {
			hasPositionalSplat = true
			break
		}
	}
	hasKeywordSplat := false
	for _, kw := range e.Keywords {
		if kw.IsSplat() {
			hasKeywordSplat = true
			break
		}
	}

	n := len(e.Args)
	if !hasPositionalSplat && n > 14 {
		c.failExpr(e, "too many arguments (maximum 14)")
		return
	}
	keywordHash := hasKeywordSplat || len(e.Keywords) > 14
	keywordCount := len(e.Keywords)
	if keywordHash {
		keywordCount = 15
	}
	keywordRegs := keywordCount * 2
	if keywordHash {
		keywordRegs = 1
	}
	positionalRegs := n
	if hasPositionalSplat {
		positionalRegs = 1
	}
	hasBlock := blockIndex >= 0 || e.BlockArg != nil
	blockRegs := 0
	if hasBlock {
		blockRegs = 1
	}

	base := s.alloc(1 + positionalRegs + keywordRegs + blockRegs)
	if !e.ImplicitSelf() {
		if preReceiver >= 0 {
			s.emitMove(base, preReceiver)
		} else {
			c.compileExpr(s, e.Receiver, base)
		}
	}
	if hasPositionalSplat {
		c.buildArgArray(s, e.Args, base+1)
	} else {
		for i, arg := range e.Args {
			c.compileExpr(s, arg, base+1+i)
		}
	}
	keywordBase := base + 1 + positionalRegs
	if keywordHash {
		c.compileKeywordHash(s, e.Keywords, keywordBase)
	} else {
		for i, kw := range e.Keywords {
			c.compileExpr(s, kw.Key, keywordBase+2*i)
			c.compileExpr(s, kw.Value, keywordBase+2*i+1)
		}
	}
	if hasBlock {
		blockReg := keywordBase + keywordRegs
		c.compileBlockOperand(s, blockIndex, e.BlockArg, blockReg)
	}

	positionalInfo := uint32(n)
	if hasPositionalSplat {
		positionalInfo = 15
	}
	argInfo := positionalInfo | uint32(keywordCount)<<4
	op := bytecode.OpSEND
	if e.ImplicitSelf() {
		op = bytecode.OpSSEND
	}
	if hasBlock {
		if e.ImplicitSelf() {
			op = bytecode.OpSSENDB
		} else {
			op = bytecode.OpSENDB
		}
	}
	s.emit(op, uint32(base), s.sym(e.Name), argInfo)
	if isSetterName(e.Name) && !e.ImplicitSelf() {
		// An attribute assignment expression evaluates to the assigned value,
		// not to the setter method's return value.
		switch {
		case hasPositionalSplat:
			s.emitMove(dst, base+1)
		case n > 0:
			s.emitMove(dst, base+n)
		default:
			s.emitMove(dst, base)
		}
		return
	}
	s.emitMove(dst, base)
}

// isSetterName reports whether name is an attribute writer such as `foo=`.
// Operator methods that end in `=` (==, <=, >=, ...) do not start with an
// identifier character and are excluded.
func isSetterName(name string) bool {
	if len(name) < 2 || name[len(name)-1] != '=' {
		return false
	}
	first := name[0]
	return first == '_' ||
		(first >= 'a' && first <= 'z') ||
		(first >= 'A' && first <= 'Z') ||
		first >= 0x80
}

func (c *Compiler) compileKeywordHash(s *scope, keywords []frontend.KeywordArg, dst int) {
	s.emit(bytecode.OpHASH, uint32(dst), 0)
	headerSP := s.sp
	for _, kw := range keywords {
		if kw.IsSplat() {
			s.setSP(dst + 2)
			c.compileExpr(s, kw.Value, dst+1)
			s.emit(bytecode.OpHASHCAT, uint32(dst))
			continue
		}
		s.setSP(dst + 3)
		c.compileExpr(s, kw.Key, dst+1)
		c.compileExpr(s, kw.Value, dst+2)
		s.emit(bytecode.OpHASHADD, uint32(dst), 1)
	}
	s.sp = headerSP
}

func (c *Compiler) compileForwardAll(s *scope, e *frontend.Call, dst int) {
	if s.method == nil || s.method.methodInfo == nil || s.yieldLevel < 0 {
		c.failExpr(e, "argument forwarding is not available here")
		return
	}
	info := s.method.methodInfo
	if info.restSlot < 0 || info.keywordSlot < 0 || info.blockSlot < 0 {
		c.failExpr(e, "argument forwarding requires a method declared with ...")
		return
	}

	base := s.alloc(4)
	if !e.ImplicitSelf() {
		c.compileExpr(s, e.Receiver, base)
	}

	arrayReg := base + 1
	keywordReg := base + 2
	blockReg := base + 3

	s.emit(bytecode.OpLOADNIL, uint32(arrayReg))
	c.emitMethodSlot(s, keywordReg, info.restSlot)
	s.emit(bytecode.OpARYCAT, uint32(arrayReg))

	s.emit(bytecode.OpHASH, uint32(keywordReg), 0)
	c.emitMethodSlot(s, blockReg, info.keywordSlot)
	s.emit(bytecode.OpHASHCAT, uint32(keywordReg))

	c.emitMethodSlot(s, blockReg, info.blockSlot)

	op := bytecode.OpSENDB
	if e.ImplicitSelf() {
		op = bytecode.OpSSENDB
	}
	argInfo := uint32(15) | 15<<4
	s.emit(op, uint32(base), s.sym(e.Name), argInfo)
	s.emitMove(dst, base)
}

func (c *Compiler) emitMethodSlot(s *scope, dst, slot int) {
	if s.yieldLevel == 0 {
		s.emitMove(dst, slot)
		return
	}
	s.emit(bytecode.OpGETUPVAR, uint32(dst), uint32(slot), uint32(s.yieldLevel-1))
}

func (c *Compiler) compileBlockOperand(s *scope, blockIndex int, blockArg frontend.Expr, reg int) {
	if blockIndex >= 0 {
		s.emit(bytecode.OpBLOCK, uint32(reg), uint32(blockIndex))
		return
	}
	if blockArg != nil {
		c.compileExpr(s, blockArg, reg)
	}
}

func (c *Compiler) buildArgArray(s *scope, args []frontend.Expr, dst int) {
	headerSP := s.sp
	first := true
	for i := 0; i < len(args); {
		if splat, ok := args[i].(*frontend.SplatArg); ok {
			if first {
				s.emit(bytecode.OpLOADNIL, uint32(dst))
				first = false
			}
			s.setSP(dst + 2)
			c.compileExpr(s, splat.Value, dst+1)
			s.emit(bytecode.OpARYCAT, uint32(dst))
			s.sp = headerSP
			i++
			continue
		}
		start := i
		for i < len(args) {
			if _, ok := args[i].(*frontend.SplatArg); ok {
				break
			}
			i++
		}
		count := i - start
		if first {
			s.setSP(dst + count)
			for j := 0; j < count; j++ {
				c.compileExpr(s, args[start+j], dst+j)
			}
			s.emit(bytecode.OpARRAY, uint32(dst), uint32(count))
			first = false
		} else {
			s.setSP(dst + count + 1)
			for j := 0; j < count; j++ {
				c.compileExpr(s, args[start+j], dst+1+j)
			}
			s.emit(bytecode.OpARYPUSH, uint32(dst), uint32(count))
		}
		s.sp = headerSP
	}
	if first {
		s.emit(bytecode.OpARRAY, uint32(dst), 0)
	}
}

func (c *Compiler) compileSuper(s *scope, e *frontend.SuperExpr, dst int) {
	var info *methodInfo
	if s.method != nil {
		info = s.method.methodInfo
	}
	if info == nil && e.Forwarding {
		c.failExpr(e, "super is not available here")
		return
	}
	if info != nil && s.yieldLevel < 0 {
		c.failExpr(e, "super is not available here")
		return
	}

	if e.Forwarding {
		level := s.yieldLevel
		if info.ainfo > 0 {
			if info.ainfo&1 != 0 && len(info.keywordParams) > 0 && !info.hasKeywordRest {
				c.rebuildKeywordHash(s, info)
			}
			base := s.alloc(4)
			aspec := uint32((info.ainfo<<4)|level) & 0xffff
			s.emit(bytecode.OpARGARY, uint32(base+1), aspec)
			blockReg := base + 2
			argInfo := uint32(15)
			if info.ainfo&1 != 0 {
				blockReg = base + 3
				argInfo |= 15 << 4
			}
			if e.Block != nil {
				child := c.compileBlock(s, e.Block)
				if child == nil {
					return
				}
				index := s.irep.AddChild(child)
				s.emit(bytecode.OpBLOCK, uint32(blockReg), uint32(index))
			} else if e.BlockArg != nil {
				c.compileExpr(s, e.BlockArg, blockReg)
			}
			s.emit(bytecode.OpSUPER, uint32(base), argInfo)
			s.emitMove(dst, base)
			return
		}

		base := s.alloc(2)
		if e.Block != nil {
			child := c.compileBlock(s, e.Block)
			if child == nil {
				return
			}
			index := s.irep.AddChild(child)
			s.emit(bytecode.OpBLOCK, uint32(base+1), uint32(index))
		} else if e.BlockArg != nil {
			c.compileExpr(s, e.BlockArg, base+1)
		} else {
			c.emitMethodSlot(s, base+1, info.blockSlot)
		}
		s.emit(bytecode.OpSUPER, uint32(base), 0)
		s.emitMove(dst, base)
		return
	}

	n := len(e.Args)
	hasPositionalSplat := false
	for _, arg := range e.Args {
		if _, ok := arg.(*frontend.SplatArg); ok {
			hasPositionalSplat = true
			break
		}
	}
	if !hasPositionalSplat && n > 14 {
		c.failExpr(e, "too many arguments to super")
		return
	}
	hasKeywordSplat := false
	for _, kw := range e.Keywords {
		if kw.IsSplat() {
			hasKeywordSplat = true
			break
		}
	}
	keywordHash := hasKeywordSplat || len(e.Keywords) > 14
	keywordCount := len(e.Keywords)
	if keywordHash {
		keywordCount = 15
	}
	keywordRegs := keywordCount * 2
	if keywordHash {
		keywordRegs = 1
	}
	positionalRegs := n
	if hasPositionalSplat {
		positionalRegs = 1
	}

	base := s.alloc(1 + positionalRegs + keywordRegs + 1)
	if hasPositionalSplat {
		c.buildArgArray(s, e.Args, base+1)
	} else {
		for i, arg := range e.Args {
			c.compileExpr(s, arg, base+1+i)
		}
	}
	keywordBase := base + 1 + positionalRegs
	if keywordHash {
		c.compileKeywordHash(s, e.Keywords, keywordBase)
	} else {
		for i, kw := range e.Keywords {
			c.compileExpr(s, kw.Key, keywordBase+2*i)
			c.compileExpr(s, kw.Value, keywordBase+2*i+1)
		}
	}
	blockReg := keywordBase + keywordRegs
	if e.Block != nil {
		child := c.compileBlock(s, e.Block)
		if child == nil {
			return
		}
		index := s.irep.AddChild(child)
		s.emit(bytecode.OpBLOCK, uint32(blockReg), uint32(index))
	} else if e.BlockArg != nil {
		c.compileExpr(s, e.BlockArg, blockReg)
	} else if info != nil {
		c.emitMethodSlot(s, blockReg, info.blockSlot)
	} else {
		s.emit(bytecode.OpLOADNIL, uint32(blockReg))
	}
	positionalInfo := uint32(n)
	if hasPositionalSplat {
		positionalInfo = 15
	}
	argInfo := positionalInfo | uint32(keywordCount)<<4
	s.emit(bytecode.OpSUPER, uint32(base), argInfo)
	s.emitMove(dst, base)
}

func (c *Compiler) compileYield(s *scope, e *frontend.YieldExpr, dst int) {
	if s.method == nil || s.method.methodInfo == nil || s.yieldLevel < 0 {
		c.failExpr(e, "yield outside a method is not supported")
		return
	}
	base := s.alloc(1 + len(e.Args))
	if len(e.Args) > 14 {
		c.failExpr(e, "too many yield arguments")
		return
	}
	hasSplat := false
	for _, arg := range e.Args {
		if _, ok := arg.(*frontend.SplatArg); ok {
			hasSplat = true
			break
		}
	}
	info := s.method.methodInfo
	aspec := uint32(((info.ainfo << 4) | s.yieldLevel) & 0xffff)
	if hasSplat {
		c.buildArgArray(s, e.Args, base+1)
		s.emit(bytecode.OpBLKPUSH, uint32(base), aspec)
		s.emit(bytecode.OpSEND, uint32(base), s.sym("call"), 15)
		s.emitMove(dst, base)
		return
	}
	for i, arg := range e.Args {
		c.compileExpr(s, arg, base+1+i)
	}
	s.emit(bytecode.OpBLKPUSH, uint32(base), aspec)
	s.emit(bytecode.OpBLKCALL, uint32(base), uint32(len(e.Args)))
	s.emitMove(dst, base)
}

func (c *Compiler) rebuildKeywordHash(s *scope, info *methodInfo) {
	if info.keywordSlot < 0 || len(info.keywordParams) == 0 || s.method == nil {
		return
	}
	sp0 := s.sp
	base := s.alloc(len(info.keywordParams) * 2)
	for i, param := range info.keywordParams {
		s.emit(bytecode.OpLOADSYM, uint32(base+2*i), s.sym(param.Name))
		slot, ok := s.method.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown keyword parameter %q", param.Name)
			return
		}
		c.emitMethodSlot(s, base+2*i+1, slot)
	}
	s.emit(bytecode.OpHASH, uint32(base), uint32(len(info.keywordParams)))
	c.emitMethodSlotSet(s, base, info.keywordSlot)
	s.sp = sp0
}

func (c *Compiler) emitMethodSlotSet(s *scope, src, slot int) {
	if s.yieldLevel == 0 {
		s.emitMove(slot, src)
		return
	}
	s.emit(bytecode.OpSETUPVAR, uint32(src), uint32(slot), uint32(s.yieldLevel-1))
}
