package bytecode

import "fmt"

// Validate checks the structural invariants the VM relies on before executing
// an irep. It is deliberately conservative: it validates frame sizes, control
// flow targets, and catch-handler ranges, but does not attempt to infer the
// meaning of every opcode operand.
func Validate(top *Irep) error {
	if top == nil {
		return fmt.Errorf("bytecode: nil irep")
	}
	for _, irep := range flattenIreps(top) {
		if irep.Nlocals > irep.Nregs {
			return fmt.Errorf("bytecode: irep has nlocals=%d > nregs=%d", irep.Nlocals, irep.Nregs)
		}
		instructions, err := Decode(irep.Code)
		if err != nil {
			return err
		}
		offsets := make(map[int]struct{}, len(instructions))
		for _, in := range instructions {
			offsets[in.Offset] = struct{}{}
		}
		for _, in := range instructions {
			var target int
			switch {
			case IsJump(in.Op):
				target, err = JumpTarget(in)
			case in.Op == OpJMPUW:
				if len(in.Operands) == 0 {
					return fmt.Errorf("bytecode: %s has no jump operand", in.Op)
				}
				target = in.Offset + in.Size + int(int16(uint16(in.Operands[0])))
			default:
				continue
			}
			if err != nil {
				return err
			}
			if _, ok := offsets[target]; !ok {
				return fmt.Errorf("bytecode: jump target %d is not an instruction boundary", target)
			}
		}
		for i, handler := range irep.Catch {
			if handler.Begin >= handler.End || int(handler.End) > len(irep.Code) {
				return fmt.Errorf("bytecode: catch handler %d has invalid range [%d,%d)", i, handler.Begin, handler.End)
			}
			if _, ok := offsets[int(handler.Target)]; !ok {
				return fmt.Errorf("bytecode: catch handler %d target %d is not an instruction boundary", i, handler.Target)
			}
		}
	}
	return nil
}

func flattenIreps(top *Irep) []*Irep {
	var out []*Irep
	var walk func(*Irep)
	walk = func(irep *Irep) {
		out = append(out, irep)
		for _, child := range irep.Reps {
			walk(child)
		}
	}
	walk(top)
	return out
}
