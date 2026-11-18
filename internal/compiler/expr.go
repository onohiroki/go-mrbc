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

	case *frontend.ClassDef:
		c.compileClass(s, e, dst)
	case *frontend.ModuleDef:
		c.compileModule(s, e, dst)
	case *frontend.SingletonClassDef:
		c.compileSingletonClass(s, e, dst)

	case *frontend.Call:
		c.compileCall(s, e, dst)
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
	case *frontend.ForExpr:
		c.compileFor(s, e, dst)
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

	if e.Rest == nil {
		for i, target := range e.Targets {
			if !c.assignIndexed(s, target, value, i, sp0) {
				return
			}
		}
		s.emitMove(dst, value)
		return
	}

	for i, target := range e.Targets {
		if !c.assignIndexed(s, target, value, i, sp0) {
			return
		}
	}
	work := s.alloc(1)
	s.emitMove(work, value)
	s.setSP(work + 1 + len(e.Posts))
	s.emit(bytecode.OpAPOST, uint32(work), uint32(len(e.Targets)), uint32(len(e.Posts)))
	if e.Rest != nil {
		if !c.assignDirect(s, *e.Rest, work) {
			return
		}
	}
	for i, target := range e.Posts {
		if !c.assignDirect(s, target, work+1+i) {
			return
		}
	}
	s.sp = sp0
	s.emitMove(dst, value)
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
	s.forDepth = parent.forDepth + 1

	aspec, _ := s.enterAspec()
	s.emit(bytecode.OpENTER, aspec)

	synthetic, ok := s.locals[name]
	if !ok {
		c.fail(e.Position(), "compiler: internal error: missing for-loop value slot")
		return nil
	}
	switch e.Target.Kind {
	case frontend.TargetLocal:
		depth := s.effectiveDepth(e.Target.Depth)
		slot, level, ok := s.resolveUpvar(e.Target.Name, depth)
		if !ok {
			c.fail(e.Target.Pos, "cannot resolve for-loop variable %q", e.Target.Name)
			return nil
		}
		s.emit(bytecode.OpSETUPVAR, uint32(synthetic), uint32(slot), uint32(level))
	case frontend.TargetGlobal:
		s.emit(bytecode.OpSETGV, uint32(synthetic), s.sym(e.Target.Name))
	case frontend.TargetInstance:
		s.emit(bytecode.OpSETIV, uint32(synthetic), s.sym(e.Target.Name))
	case frontend.TargetConstant:
		s.emit(bytecode.OpSETCONST, uint32(synthetic), s.sym(e.Target.Name))
	default:
		c.fail(e.Target.Pos, "unsupported for-loop target")
		return nil
	}

	sp0 := s.sp
	if len(e.Body) > 0 {
		dst := s.alloc(1)
		c.compileBody(s, e.Body, dst)
		s.sp = sp0
	}
	s.emit(bytecode.OpRETNIL)

	if err := s.finish(); err != nil {
		c.fail(e.Position(), "%v", err)
		return nil
	}
	return s.irep
}

func (c *Compiler) compileCall(s *scope, e *frontend.Call, dst int) {
	if e.Block != nil && e.BlockArg != nil {
		c.failExpr(e, "both a block and a block argument are not allowed")
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

	hasSplat := false
	for _, arg := range e.Args {
		if _, ok := arg.(*frontend.SplatArg); ok {
			hasSplat = true
			break
		}
	}

	if hasSplat {
		if len(e.Keywords) > 0 || e.KeywordSplat != nil {
			c.failExpr(e, "keyword arguments combined with splat arguments are not supported yet")
			return
		}
		hasBlock := blockIndex >= 0 || e.BlockArg != nil
		base := s.alloc(1)
		if !e.ImplicitSelf() {
			c.compileExpr(s, e.Receiver, base)
		}
		arrayReg := s.alloc(1)
		c.buildArgArray(s, e.Args, arrayReg)
		if hasBlock {
			blockReg := s.alloc(1)
			c.compileBlockOperand(s, blockIndex, e.BlockArg, blockReg)
		}
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
		s.emit(op, uint32(base), s.sym(e.Name), 15)
		s.emitMove(dst, base)
		return
	}

	const maxArgs = 14
	n := len(e.Args)
	if n > maxArgs {
		c.failExpr(e, "too many arguments (maximum %d)", maxArgs)
		return
	}
	keywordCount := len(e.Keywords)
	keywordHash := false
	if e.KeywordSplat != nil {
		if keywordCount > 0 {
			c.failExpr(e, "keyword splats combined with explicit keywords are not supported yet")
			return
		}
		keywordHash = true
		keywordCount = 15
	}
	keywordRegs := 0
	if keywordHash {
		keywordRegs = 1
	} else {
		keywordRegs = keywordCount * 2
	}
	hasBlock := blockIndex >= 0 || e.BlockArg != nil
	extra := 0
	if hasBlock {
		extra = 1
	}

	base := s.alloc(1 + n + keywordRegs + extra)
	if !e.ImplicitSelf() {
		c.compileExpr(s, e.Receiver, base)
	}
	for i, arg := range e.Args {
		c.compileExpr(s, arg, base+1+i)
	}
	keywordBase := base + 1 + n
	if keywordHash {
		c.compileExpr(s, e.KeywordSplat, keywordBase)
	} else {
		for i, pair := range e.Keywords {
			c.compileExpr(s, pair.Key, keywordBase+2*i)
			c.compileExpr(s, pair.Value, keywordBase+2*i+1)
		}
	}
	if hasBlock {
		blockReg := base + 1 + n + keywordRegs
		c.compileBlockOperand(s, blockIndex, e.BlockArg, blockReg)
	}

	argInfo := uint32(n) | uint32(keywordCount)<<4
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
	s.emitMove(dst, base)
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
	if s.yieldLevel != 0 || s.method == nil || s.method.methodInfo == nil {
		c.failExpr(e, "super is only supported directly in a method body")
		return
	}
	info := s.method.methodInfo

	if e.Forwarding {
		if info.ainfo > 0 {
			if info.ainfo&1 != 0 && len(info.keywordParams) > 0 {
				if info.hasKeywordRest {
					c.failExpr(e, "super forwarding with both named keywords and **rest is not supported yet")
					return
				}
				c.rebuildKeywordHash(s, info)
			}
			base := s.alloc(4)
			aspec := uint32(info.ainfo<<4) & 0xffff
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
			s.emitMove(base+1, info.blockSlot)
		}
		s.emit(bytecode.OpSUPER, uint32(base), 0)
		s.emitMove(dst, base)
		return
	}

	n := len(e.Args)
	hasSplat := false
	for _, arg := range e.Args {
		if _, ok := arg.(*frontend.SplatArg); ok {
			hasSplat = true
			break
		}
	}
	if !hasSplat && n > 14 {
		c.failExpr(e, "too many arguments to super")
		return
	}
	if hasSplat {
		base := s.alloc(3)
		c.buildArgArray(s, e.Args, base+1)
		blockReg := base + 2
		if e.Block != nil {
			child := c.compileBlock(s, e.Block)
			if child == nil {
				return
			}
			index := s.irep.AddChild(child)
			s.emit(bytecode.OpBLOCK, uint32(blockReg), uint32(index))
		} else if e.BlockArg != nil {
			c.compileExpr(s, e.BlockArg, blockReg)
		} else {
			s.emitMove(blockReg, info.blockSlot)
		}
		s.emit(bytecode.OpSUPER, uint32(base), 15)
		s.emitMove(dst, base)
		return
	}
	base := s.alloc(1 + n + 1)
	for i, arg := range e.Args {
		c.compileExpr(s, arg, base+1+i)
	}
	blockReg := base + 1 + n
	if e.Block != nil {
		child := c.compileBlock(s, e.Block)
		if child == nil {
			return
		}
		index := s.irep.AddChild(child)
		s.emit(bytecode.OpBLOCK, uint32(blockReg), uint32(index))
	} else if e.BlockArg != nil {
		c.compileExpr(s, e.BlockArg, blockReg)
	} else {
		s.emitMove(blockReg, info.blockSlot)
	}
	s.emit(bytecode.OpSUPER, uint32(base), uint32(n))
	s.emitMove(dst, base)
}

func (c *Compiler) compileYield(s *scope, e *frontend.YieldExpr, dst int) {
	if s.method == nil || s.method.methodInfo == nil || s.yieldLevel < 0 {
		c.failExpr(e, "yield outside a method is not supported")
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
	aspec := uint32(((info.ainfo << 4) | s.yieldLevel) & 0xffff)
	s.emit(bytecode.OpBLKPUSH, uint32(base), aspec)
	s.emit(bytecode.OpBLKCALL, uint32(base), uint32(len(e.Args)))
	s.emitMove(dst, base)
}

func (c *Compiler) rebuildKeywordHash(s *scope, info *methodInfo) {
	if info.keywordSlot < 0 || len(info.keywordParams) == 0 {
		return
	}
	sp0 := s.sp
	base := s.alloc(len(info.keywordParams) * 2)
	for i, param := range info.keywordParams {
		s.emit(bytecode.OpLOADSYM, uint32(base+2*i), s.sym(param.Name))
		slot, ok := s.locals[param.Name]
		if !ok {
			c.fail(param.Pos, "compiler: unknown keyword parameter %q", param.Name)
			return
		}
		s.emitMove(base+2*i+1, slot)
	}
	s.emit(bytecode.OpHASH, uint32(base), uint32(len(info.keywordParams)))
	s.emitMove(info.keywordSlot, base)
	s.sp = sp0
}
