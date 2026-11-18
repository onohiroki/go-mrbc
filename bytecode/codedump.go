package bytecode

import (
	"fmt"
	"io"
	"strings"
)

// Disassemble writes a human-readable instruction listing for top and its
// child ireps. The format is intentionally close to mruby's `mrbc -v` output
// but is not required to match it byte for byte.
func Disassemble(w io.Writer, top *Irep) error {
	counter := 0
	return disassembleIrep(w, top, &counter)
}

func disassembleIrep(w io.Writer, irep *Irep, counter *int) error {
	if irep == nil {
		return fmt.Errorf("bytecode: nil irep")
	}
	index := *counter
	*counter++
	if _, err := fmt.Fprintf(w,
		"irep[%d] nlocals=%d nregs=%d pool=%d syms=%d reps=%d ilen=%d\n",
		index, irep.Nlocals, irep.Nregs, len(irep.Pool), len(irep.Syms),
		len(irep.Reps), len(irep.Code)); err != nil {
		return err
	}

	instructions, err := Decode(irep.Code)
	if err != nil {
		return err
	}
	for _, in := range instructions {
		if _, err := fmt.Fprintf(w, "  %04d  %-12s %s\n",
			in.Offset, in.Op.String(), formatOperands(irep, in)); err != nil {
			return err
		}
	}

	for _, child := range irep.Reps {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if err := disassembleIrep(w, child, counter); err != nil {
			return err
		}
	}
	return nil
}

func formatOperands(irep *Irep, in Instruction) string {
	op := in.Op
	ops := in.Operands

	sym := func(index int) string {
		if index < 0 || index >= len(irep.Syms) {
			return fmt.Sprintf("?%d", index)
		}
		return ":" + irep.Syms[index]
	}
	pool := func(index int) string {
		if index < 0 || index >= len(irep.Pool) {
			return fmt.Sprintf("?L%d", index)
		}
		entry := irep.Pool[index]
		switch entry.Kind {
		case PoolString:
			return fmt.Sprintf("L%d %q", index, entry.String)
		case PoolInt32, PoolInt64:
			return fmt.Sprintf("L%d %d", index, entry.Int)
		case PoolFloat:
			return fmt.Sprintf("L%d %v", index, entry.Float)
		default:
			return fmt.Sprintf("L%d", index)
		}
	}
	rep := func(index int) string {
		return fmt.Sprintf("I[%d]", index)
	}
	number := func(index int) string {
		if index < 0 || index >= len(ops) {
			return "?"
		}
		return fmt.Sprintf("%d", ops[index])
	}
	reg := func(index int) string {
		if index < 0 || index >= len(ops) {
			return "R?"
		}
		return fmt.Sprintf("R%d", ops[index])
	}

	switch op {
	case OpLOADSYM, OpGETGV, OpSETGV, OpGETIV, OpSETIV, OpGETCV, OpSETCV,
		OpGETCONST, OpSETCONST, OpGETMCNST, OpSETMCNST, OpKARG, OpKEY_P, OpDEF:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), sym(int(ops[1])))
		}
	case OpSEND, OpSEND0, OpSSEND, OpSSEND0, OpSENDB, OpSSENDB:
		if len(ops) == 3 {
			return fmt.Sprintf("%s %s n=%d", reg(0), sym(int(ops[1])), ops[2]&0xf)
		}
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), sym(int(ops[1])))
		}
	case OpTDEF, OpSDEF:
		if len(ops) == 3 {
			return fmt.Sprintf("%s %s %s", reg(0), sym(int(ops[1])), rep(int(ops[2])))
		}
	case OpBLOCK, OpLAMBDA, OpMETHOD, OpEXEC:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), rep(int(ops[1])))
		}
	case OpLOADL:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), pool(int(ops[1])))
		}
	case OpSTRING, OpSYMBOL:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), pool(int(ops[1])))
		}
	case OpGETUPVAR, OpSETUPVAR:
		if len(ops) == 3 {
			return fmt.Sprintf("%s slot=%d level=%d", reg(0), ops[1], ops[2])
		}
	case OpJMP:
		if len(ops) == 1 {
			if target, err := JumpTarget(in); err == nil {
				return fmt.Sprintf("-> %04d", target)
			}
		}
	case OpJMPIF, OpJMPNOT, OpJMPNIL:
		if len(ops) == 2 {
			if target, err := JumpTarget(in); err == nil {
				return fmt.Sprintf("%s -> %04d", reg(0), target)
			}
		}
	case OpENTER:
		if len(ops) == 1 {
			return fmt.Sprintf("aspec=0x%06x", ops[0])
		}
	case OpARGARY, OpBLKPUSH:
		if len(ops) == 2 {
			return fmt.Sprintf("%s aspec=0x%04x", reg(0), ops[1])
		}
	case OpALIAS:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", sym(int(ops[0])), sym(int(ops[1])))
		}
	case OpUNDEF:
		if len(ops) == 1 {
			return sym(int(ops[0]))
		}
	case OpLOADI8, OpLOADI16, OpLOADI32, OpLOADINEG:
		if len(ops) == 2 {
			return fmt.Sprintf("%s %s", reg(0), number(1))
		}
	}

	parts := make([]string, len(ops))
	for i, value := range ops {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, " ")
}
