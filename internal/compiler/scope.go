package compiler

import (
	"fmt"
	"math"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

type scopeKind uint8

const (
	scopeTop scopeKind = iota
	scopeMethod
	scopeBlock
)

type methodInfo struct {
	aspec uint32
	ainfo int
}

type loopKind uint8

const (
	loopWhile loopKind = iota
)

type loopInfo struct {
	kind           loopKind
	breakTarget    label
	continueTarget label
	resultReg      int
}

type scope struct {
	c      *Compiler
	parent *scope
	kind   scopeKind
	irep   *bytecode.Irep
	asm    *assembler

	locals  map[string]int
	params  []frontend.Param
	nlocals int
	// sp is the first free temporary register, and maxsp is its high-water
	// mark. Temporary registers always live at or above nlocals.
	sp    int
	maxsp int

	method     *scope
	methodInfo *methodInfo
	yieldLevel int // block scopes between this scope and its method, or -1
	loop       *loopInfo
}

func (c *Compiler) newScope(parent *scope, kind scopeKind, params []frontend.Param, locals []string) *scope {
	s := &scope{
		c:      c,
		parent: parent,
		kind:   kind,
		irep:   &bytecode.Irep{},
		asm:    newAssembler(),
		locals: make(map[string]int),
		params: params,
	}

	if parent != nil {
		s.method = parent.method
		s.yieldLevel = parent.yieldLevel
	}
	switch kind {
	case scopeMethod:
		s.method = s
		s.yieldLevel = 0
	case scopeBlock:
		switch {
		case parent == nil:
			s.yieldLevel = -1
		case parent.kind == scopeMethod:
			s.yieldLevel = 1
		case parent.kind == scopeBlock && parent.yieldLevel >= 0:
			s.yieldLevel = parent.yieldLevel + 1
		default:
			s.yieldLevel = -1
		}
	}

	if err := s.initLocals(locals); err != nil {
		c.fail(frontend.Position{}, "%s", err.Error())
	}
	return s
}

func (s *scope) initLocals(locals []string) error {
	next := 1 // R0 is self
	add := func(name string) error {
		if name == "" {
			return fmt.Errorf("compiler: empty local variable name")
		}
		if _, exists := s.locals[name]; exists {
			return nil
		}
		if next > math.MaxUint16 {
			return fmt.Errorf("compiler: too many local variables")
		}
		s.locals[name] = next
		next++
		return nil
	}

	positional := 0
	var blockParam *frontend.Param
	for i := range s.params {
		param := &s.params[i]
		if param.Kind == frontend.ParamBlock {
			blockParam = param
			continue
		}
		if err := add(param.Name); err != nil {
			return err
		}
		positional++
	}

	if s.kind == scopeMethod || s.kind == scopeBlock {
		blockPos := 1 + positional
		if blockParam != nil {
			if err := add(blockParam.Name); err != nil {
				return err
			}
			if s.locals[blockParam.Name] != blockPos {
				return fmt.Errorf("compiler: block parameter %q is not in the block slot", blockParam.Name)
			}
		} else {
			// The VM always stores the incoming block in this register.
			next = blockPos + 1
		}
	}

	for _, name := range locals {
		if err := add(name); err != nil {
			return err
		}
	}

	s.nlocals = next
	s.sp = next
	s.maxsp = next
	return nil
}

func (s *scope) alloc(n int) int {
	base := s.sp
	s.sp += n
	if s.sp > s.maxsp {
		s.maxsp = s.sp
	}
	return base
}

func (s *scope) emit(op bytecode.Opcode, operands ...uint32) {
	s.asm.emit(op, operands...)
}

func (s *scope) emitMove(dst, src int) {
	if dst != src {
		s.emit(bytecode.OpMOVE, uint32(dst), uint32(src))
	}
}

func (s *scope) sym(name string) uint32 {
	return uint32(s.irep.SymbolIndex(name))
}

func (s *scope) finish() error {
	if err := s.asm.patch(); err != nil {
		return err
	}
	if s.nlocals > math.MaxUint16 {
		return fmt.Errorf("compiler: too many locals: %d", s.nlocals)
	}
	nregs := s.maxsp
	if nregs < s.nlocals {
		nregs = s.nlocals
	}
	if nregs > math.MaxUint16 {
		return fmt.Errorf("compiler: too many registers: %d", nregs)
	}
	s.irep.Code = s.asm.code
	s.irep.Nlocals = uint16(s.nlocals)
	s.irep.Nregs = uint16(nregs)
	return nil
}

func (s *scope) resolveUpvar(name string, depth int) (slot int, level int, ok bool) {
	cur := s
	for i := 0; i < depth; i++ {
		if cur.parent == nil {
			return 0, 0, false
		}
		cur = cur.parent
	}
	slot, ok = cur.locals[name]
	if !ok {
		return 0, 0, false
	}
	return slot, depth - 1, true
}

func (s *scope) enterAspec() (uint32, int) {
	var req, opt, rest, block int
	for _, param := range s.params {
		switch param.Kind {
		case frontend.ParamRequired:
			req++
		case frontend.ParamOptional:
			opt++
		case frontend.ParamRest:
			rest = 1
		case frontend.ParamBlock:
			block = 1
		}
	}
	aspec := uint32(req<<18 | opt<<13 | rest<<12 | block)
	ainfo := ((req + opt) & 0x3f) << 7
	ainfo |= rest << 6
	return aspec, ainfo
}
