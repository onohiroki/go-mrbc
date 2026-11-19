package compiler

import (
	"fmt"
	"math"
	"strings"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

type scopeKind uint8

const (
	scopeTop scopeKind = iota
	scopeMethod
	scopeBlock
	scopeClass
	scopeLambda
)

type methodInfo struct {
	aspec          uint32
	ainfo          int
	blockSlot      int
	keywordSlot    int
	restSlot       int
	keywordParams  []frontend.Param
	hasKeywordRest bool
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

	blockSlot   int
	keywordSlot int
	restSlot    int
	forDepth    int
	forScope    bool

	rescue     *rescueContext
	redoTarget label
	hasRedo    bool

	lines []bytecode.DebugLine
}

type rescueContext struct {
	retryLabel label
}

func (c *Compiler) newScope(parent *scope, kind scopeKind, params []frontend.Param, locals []string) *scope {
	s := &scope{
		c:           c,
		parent:      parent,
		kind:        kind,
		irep:        &bytecode.Irep{},
		asm:         newAssembler(),
		locals:      make(map[string]int),
		params:      params,
		blockSlot:   -1,
		keywordSlot: -1,
		restSlot:    -1,
	}

	if parent != nil {
		s.method = parent.method
		s.yieldLevel = parent.yieldLevel
		s.forDepth = parent.forDepth
	}
	switch kind {
	case scopeMethod:
		s.method = s
		s.yieldLevel = 0
		s.forDepth = 0
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
	case scopeClass, scopeLambda:
		s.method = nil
		s.yieldLevel = -1
		if kind == scopeClass {
			s.forDepth = 0
		}
	}

	if err := s.initLocals(locals); err != nil {
		c.fail(frontend.Position{}, "%s", err.Error())
	}
	return s
}

func (s *scope) initLocals(locals []string) error {
	next := 1 // R0 is self
	bind := func(name string, slot int) error {
		if name == "" {
			return fmt.Errorf("compiler: empty local variable name")
		}
		if existing, exists := s.locals[name]; exists {
			if existing != slot {
				return fmt.Errorf("compiler: duplicate local variable %q", name)
			}
			return nil
		}
		if slot > math.MaxUint16 {
			return fmt.Errorf("compiler: too many local variables")
		}
		s.locals[name] = slot
		return nil
	}
	add := func(name string) error {
		if _, exists := s.locals[name]; exists {
			return nil
		}
		if err := bind(name, next); err != nil {
			return err
		}
		next++
		return nil
	}

	positional := 0
	hasKeywords := false
	var keywordRest *frontend.Param
	var blockParam *frontend.Param
	for i := range s.params {
		param := &s.params[i]
		switch param.Kind {
		case frontend.ParamRequired, frontend.ParamOptional, frontend.ParamPost:
			if err := add(param.Name); err != nil {
				return err
			}
			positional++
		case frontend.ParamDestructure:
			name := fmt.Sprintf("__arg_destructure_%d", i)
			param.Name = name
			if err := bind(name, next); err != nil {
				return err
			}
			next++
			positional++
		case frontend.ParamRest:
			slot := next
			name := param.Name
			if name == "" {
				// mruby names an unnamed rest parameter "*" in the LVAR
				// section so that Proc#parameters can report it.
				name = "*"
			}
			if err := bind(name, slot); err != nil {
				return err
			}
			s.restSlot = slot
			next++
			positional++
		case frontend.ParamKeyword, frontend.ParamOptionalKeyword:
			hasKeywords = true
		case frontend.ParamKeywordRest:
			hasKeywords = true
			keywordRest = param
		case frontend.ParamBlock, frontend.ParamNoBlock:
			blockParam = param
		}
	}

	if s.kind == scopeMethod || s.kind == scopeBlock || s.kind == scopeLambda {
		if hasKeywords {
			keywordSlot := 1 + positional
			s.keywordSlot = keywordSlot
			next = keywordSlot + 1
			keywordName := "**"
			if keywordRest != nil && keywordRest.Name != "" {
				keywordName = keywordRest.Name
			}
			if err := bind(keywordName, keywordSlot); err != nil {
				return err
			}
			blockSlot := keywordSlot + 1
			s.blockSlot = blockSlot
			next = blockSlot + 1
			if blockParam != nil && blockParam.Name != "" {
				// mruby keeps the internal block in its own slot and copies
				// it into the named parameter's slot.
				if err := bind(blockParam.Name, blockSlot+1); err != nil {
					return err
				}
				next = blockSlot + 2
			}
			for i := range s.params {
				param := &s.params[i]
				if param.Kind == frontend.ParamKeyword || param.Kind == frontend.ParamOptionalKeyword {
					if err := add(param.Name); err != nil {
						return err
					}
				}
			}
		} else {
			blockSlot := 1 + positional
			s.blockSlot = blockSlot
			next = blockSlot + 1
			if blockParam != nil && blockParam.Name != "" {
				if err := bind(blockParam.Name, blockSlot+1); err != nil {
					return err
				}
				next = blockSlot + 2
			}
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

func (s *scope) setSP(reg int) {
	s.sp = reg
	if reg > s.maxsp {
		s.maxsp = reg
	}
}

func (s *scope) emit(op bytecode.Opcode, operands ...uint32) {
	s.asm.emit(op, operands...)
}

func (s *scope) emitMove(dst, src int) {
	if dst != src {
		s.emit(bytecode.OpMOVE, uint32(dst), uint32(src))
	}
}

func (s *scope) markLine(pos frontend.Position) {
	if s.c.err != nil || pos.Line <= 0 {
		return
	}
	line := int32(pos.Line)
	offset := int32(len(s.asm.code))
	if len(s.lines) > 0 {
		last := s.lines[len(s.lines)-1]
		if last.Line == line {
			return
		}
		if last.Offset == offset {
			s.lines[len(s.lines)-1].Line = line
			return
		}
	}
	s.lines = append(s.lines, bytecode.DebugLine{Offset: offset, Line: line})
}

func (s *scope) sym(name string) uint32 {
	return uint32(s.irep.SymbolIndex(name))
}

func (s *scope) finish() error {
	if err := s.asm.patch(); err != nil {
		return err
	}
	for _, ref := range s.asm.catches {
		begin, ok := s.asm.labels[ref.begin]
		if !ok {
			return fmt.Errorf("compiler: unbound catch begin label %d", ref.begin)
		}
		end, ok := s.asm.labels[ref.end]
		if !ok {
			return fmt.Errorf("compiler: unbound catch end label %d", ref.end)
		}
		target, ok := s.asm.labels[ref.target]
		if !ok {
			return fmt.Errorf("compiler: unbound catch target label %d", ref.target)
		}
		s.irep.Catch = append(s.irep.Catch, bytecode.CatchHandler{
			Type:   ref.typ,
			Begin:  uint32(begin),
			End:    uint32(end),
			Target: uint32(target),
		})
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
	s.irep.Filename = s.c.source.File
	s.irep.Lines = s.lines
	names := make([]string, s.nlocals)
	for name, slot := range s.locals {
		if slot > 0 && slot < len(names) &&
			!strings.HasPrefix(name, "__gomrbc_for_") &&
			!strings.HasPrefix(name, "__arg_destructure_") {
			names[slot] = name
		}
	}
	s.irep.LocalNames = names
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

func (s *scope) effectiveDepth(depth int) int {
	if !s.forScope && depth == 0 {
		return 0
	}
	return depth + s.forDepth
}

func (s *scope) enterAspec() (uint32, int) {
	var req, opt, rest, post, keywords, keywordRest, block, noBlock int
	for _, param := range s.params {
		switch param.Kind {
		case frontend.ParamRequired:
			req++
		case frontend.ParamDestructure:
			req++
		case frontend.ParamOptional:
			opt++
		case frontend.ParamRest:
			rest = 1
		case frontend.ParamPost:
			post++
		case frontend.ParamKeyword, frontend.ParamOptionalKeyword:
			keywords++
		case frontend.ParamKeywordRest:
			keywordRest = 1
		case frontend.ParamBlock:
			block = 1
		case frontend.ParamNoBlock:
			noBlock = 1
		}
	}
	if req > 31 || opt > 31 || post > 31 || keywords > 31 || req+opt > 63 {
		s.c.fail(frontend.Position{}, "compiler: too many formal arguments")
		return 0, -1
	}
	aspec := uint32(noBlock<<23 | req<<18 | opt<<13 | rest<<12 | post<<7 | keywords<<2 | keywordRest<<1 | block)
	ainfo := ((req + opt) & 0x3f) << 7
	ainfo |= rest << 6
	ainfo |= post << 1
	if keywords > 0 || keywordRest != 0 {
		ainfo |= 1
	}
	return aspec, ainfo
}
