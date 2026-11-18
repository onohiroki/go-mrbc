package bytecode

import (
	"fmt"
)

// Opcode is an mruby 4.0.0 instruction opcode.
type Opcode uint8

// OperandKind describes the operand layout of an opcode.
type OperandKind uint8

const (
	OperandZ OperandKind = iota
	OperandB
	OperandBB
	OperandBBB
	OperandBS
	OperandBSS
	OperandS
	OperandW
)

type opcodeDesc struct {
	name     string
	operands OperandKind
}

// Valid reports whether op is a known mruby 4.0.0 opcode.
func (op Opcode) Valid() bool {
	return int(op) < OpcodeCount
}

// String returns the name of the opcode.
func (op Opcode) String() string {
	if !op.Valid() {
		return fmt.Sprintf("OP_%d", uint8(op))
	}
	return opcodeInfo[op].name
}

// OperandKind returns the operand layout of the opcode.
func (op Opcode) OperandKind() OperandKind {
	if !op.Valid() {
		return OperandZ
	}
	return opcodeInfo[op].operands
}

// operandWidths returns the base widths, in bytes, of each operand.
func operandWidths(kind OperandKind) []int {
	switch kind {
	case OperandZ:
		return nil
	case OperandB:
		return []int{1}
	case OperandBB:
		return []int{1, 1}
	case OperandBBB:
		return []int{1, 1, 1}
	case OperandBS:
		return []int{1, 2}
	case OperandBSS:
		return []int{1, 2, 2}
	case OperandS:
		return []int{2}
	case OperandW:
		return []int{3}
	default:
		return nil
	}
}

func operandCount(kind OperandKind) int {
	return len(operandWidths(kind))
}

// Instruction is a decoded instruction. Offset is the byte offset of the
// instruction (including an EXT prefix, if any) within the instruction
// sequence. Size includes the prefix and all operands.
type Instruction struct {
	Offset   int
	Prefix   Opcode
	Op       Opcode
	Operands []uint32
	Size     int
}

// IsExt reports whether the instruction used an EXT prefix.
func (in Instruction) IsExt() bool {
	return in.Prefix == OpEXT1 || in.Prefix == OpEXT2 || in.Prefix == OpEXT3
}

// InstructionLen returns the encoded length of an instruction.
func InstructionLen(op Opcode) int {
	return 1 + operandBytes(op)
}

func operandBytes(op Opcode) int {
	widths := operandWidths(op.OperandKind())
	total := 0
	for _, width := range widths {
		total += width
	}
	return total
}

// EncodeInstruction appends the encoded form of op and its operands to dst.
// It returns an error if the opcode is unknown, the operand count is wrong, or
// an operand does not fit its available width.
func EncodeInstruction(dst []byte, op Opcode, operands ...uint32) ([]byte, error) {
	if !op.Valid() {
		return nil, fmt.Errorf("bytecode: unknown opcode %d", uint8(op))
	}
	kind := op.OperandKind()
	widths := operandWidths(kind)
	if len(operands) != len(widths) {
		return nil, fmt.Errorf("bytecode: %s expects %d operands, got %d", op, len(widths), len(operands))
	}

	extend0, extend1 := false, false
	if len(widths) > 0 && widths[0] == 1 && operands[0] > 0xff {
		extend0 = true
	}
	if len(widths) > 1 && widths[1] == 1 && operands[1] > 0xff {
		extend1 = true
	}

	prefix := OpNOP
	switch {
	case extend0 && extend1:
		prefix = OpEXT3
	case extend0:
		prefix = OpEXT1
	case extend1:
		prefix = OpEXT2
	}
	if prefix != OpNOP {
		dst = append(dst, byte(prefix))
	}
	dst = append(dst, byte(op))

	for i, width := range widths {
		value := operands[i]
		if (i == 0 && extend0) || (i == 1 && extend1) {
			width = 2
		}
		switch width {
		case 1:
			if value > 0xff {
				return nil, fmt.Errorf("bytecode: %s operand %d (%d) exceeds 8 bits", op, i, value)
			}
			dst = append(dst, byte(value))
		case 2:
			if value > 0xffff {
				return nil, fmt.Errorf("bytecode: %s operand %d (%d) exceeds 16 bits", op, i, value)
			}
			dst = append(dst, byte(value>>8), byte(value))
		case 3:
			if value > 0xffffff {
				return nil, fmt.Errorf("bytecode: %s operand %d (%d) exceeds 24 bits", op, i, value)
			}
			dst = append(dst, byte(value>>16), byte(value>>8), byte(value))
		default:
			return nil, fmt.Errorf("bytecode: %s has unsupported operand width %d", op, width)
		}
	}
	return dst, nil
}

// DecodeInstruction decodes the instruction at offset pc.
func DecodeInstruction(code []byte, pc int) (Instruction, error) {
	if pc < 0 || pc >= len(code) {
		return Instruction{}, fmt.Errorf("bytecode: program counter %d out of range", pc)
	}

	start := pc
	prefix := OpNOP
	op := Opcode(code[pc])
	pc++
	if op == OpEXT1 || op == OpEXT2 || op == OpEXT3 {
		prefix = op
		if pc >= len(code) {
			return Instruction{}, fmt.Errorf("bytecode: truncated EXT prefix at %d", start)
		}
		op = Opcode(code[pc])
		pc++
	}
	if !op.Valid() {
		return Instruction{}, fmt.Errorf("bytecode: unknown opcode %d at %d", uint8(op), start)
	}

	kind := op.OperandKind()
	widths := operandWidths(kind)
	operands := make([]uint32, 0, len(widths))
	for i, width := range widths {
		if (prefix == OpEXT1 || prefix == OpEXT3) && i == 0 {
			width = 2
		}
		if (prefix == OpEXT2 || prefix == OpEXT3) && i == 1 {
			width = 2
		}
		if pc+width > len(code) {
			return Instruction{}, fmt.Errorf("bytecode: truncated %s at %d", op, start)
		}
		var value uint32
		for j := 0; j < width; j++ {
			value = value<<8 | uint32(code[pc+j])
		}
		pc += width
		operands = append(operands, value)
	}

	return Instruction{
		Offset:   start,
		Prefix:   prefix,
		Op:       op,
		Operands: operands,
		Size:     pc - start,
	}, nil
}

// Decode decodes every instruction in code.
func Decode(code []byte) ([]Instruction, error) {
	var instructions []Instruction
	for pc := 0; pc < len(code); {
		in, err := DecodeInstruction(code, pc)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, in)
		pc += in.Size
	}
	return instructions, nil
}

// IsJump reports whether op is a control-flow instruction whose first operand
// is a signed 16-bit offset relative to the end of the instruction.
func IsJump(op Opcode) bool {
	switch op {
	case OpJMP, OpJMPIF, OpJMPNOT, OpJMPNIL:
		return true
	default:
		return false
	}
}

// JumpTarget returns the absolute instruction offset targeted by a jump. The
// instruction must have been decoded from code and must satisfy IsJump.
func JumpTarget(in Instruction) (int, error) {
	if !IsJump(in.Op) {
		return 0, fmt.Errorf("bytecode: %s is not a jump", in.Op)
	}
	if len(in.Operands) == 0 {
		return 0, fmt.Errorf("bytecode: %s has no jump operand", in.Op)
	}
	offsetIndex := 0
	if in.Op != OpJMP {
		offsetIndex = 1
	}
	if offsetIndex >= len(in.Operands) {
		return 0, fmt.Errorf("bytecode: %s has no jump offset", in.Op)
	}
	return in.Offset + in.Size + int(int16(uint16(in.Operands[offsetIndex]))), nil
}
