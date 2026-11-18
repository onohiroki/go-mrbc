package compiler

import (
	"fmt"

	"github.com/onohiroki/go-mrbc/bytecode"
)

type label int

type fixup struct {
	pos   int // position of the 16-bit operand
	base  int // end of the instruction
	label label
}

type catchRef struct {
	typ    uint8
	begin  label
	end    label
	target label
}

type assembler struct {
	code      []byte
	labels    map[label]int
	fixups    []fixup
	catches   []catchRef
	nextLabel label
	err       error
}

func newAssembler() *assembler {
	return &assembler{labels: make(map[label]int)}
}

func (a *assembler) emit(op bytecode.Opcode, operands ...uint32) {
	if a.err != nil {
		return
	}
	code, err := bytecode.EncodeInstruction(a.code, op, operands...)
	if err != nil {
		a.err = err
		return
	}
	a.code = code
}

func (a *assembler) newLabel() label {
	l := a.nextLabel
	a.nextLabel++
	return l
}

func (a *assembler) bind(l label) {
	if _, exists := a.labels[l]; exists {
		a.err = fmt.Errorf("compiler: label %d bound twice", l)
		return
	}
	a.labels[l] = len(a.code)
}

func (a *assembler) jmp(l label) {
	if a.err != nil {
		return
	}
	start := len(a.code)
	a.emit(bytecode.OpJMP, 0)
	a.fixups = append(a.fixups, fixup{pos: start + 1, base: start + 3, label: l})
}

func (a *assembler) jmpuw(l label) {
	if a.err != nil {
		return
	}
	start := len(a.code)
	a.emit(bytecode.OpJMPUW, 0)
	a.fixups = append(a.fixups, fixup{pos: start + 1, base: start + 3, label: l})
}

func (a *assembler) addCatch(typ uint8, begin, end, target label) {
	a.catches = append(a.catches, catchRef{typ: typ, begin: begin, end: end, target: target})
}

func (a *assembler) jmpIf(op bytecode.Opcode, reg int, l label) {
	if a.err != nil {
		return
	}
	start := len(a.code)
	a.emit(op, uint32(reg), 0)
	a.fixups = append(a.fixups, fixup{pos: start + 2, base: start + 4, label: l})
}

func (a *assembler) patch() error {
	if a.err != nil {
		return a.err
	}
	for _, f := range a.fixups {
		target, ok := a.labels[f.label]
		if !ok {
			return fmt.Errorf("compiler: unbound label %d", f.label)
		}
		delta := target - f.base
		if delta < -32768 || delta > 32767 {
			return fmt.Errorf("compiler: jump out of range (%d bytes)", delta)
		}
		value := uint16(int16(delta))
		a.code[f.pos] = byte(value >> 8)
		a.code[f.pos+1] = byte(value)
	}
	return nil
}
