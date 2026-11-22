package mrbc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/mrbc"
)

func compileIrep(t *testing.T, src string) *bytecode.Irep {
	t.Helper()
	irep, err := mrbc.Compile(context.Background(), []byte(src), mrbc.Options{FilePath: "ops.rb"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return irep
}

func compileInstructions(t *testing.T, src string) []bytecode.Instruction {
	t.Helper()
	return decodeCode(t, compileIrep(t, src).Code)
}

func decodeCode(t *testing.T, code []byte) []bytecode.Instruction {
	t.Helper()
	instructions, err := bytecode.Decode(code)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return instructions
}

// allInstructions returns the instructions of irep and every nested irep.
func allInstructions(t *testing.T, irep *bytecode.Irep) []bytecode.Instruction {
	t.Helper()
	instructions := decodeCode(t, irep.Code)
	for _, child := range irep.Reps {
		instructions = append(instructions, allInstructions(t, child)...)
	}
	return instructions
}

func countOpcode(instructions []bytecode.Instruction, op bytecode.Opcode) int {
	count := 0
	for _, in := range instructions {
		if in.Op == op {
			count++
		}
	}
	return count
}

func hasOpcode(instructions []bytecode.Instruction, op bytecode.Opcode) bool {
	return countOpcode(instructions, op) > 0
}

func TestOperatorOpcodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bytecode.Opcode
	}{
		{"add", "x = 1\ny = 2\nx + y\n", bytecode.OpADD},
		{"sub", "x = 1\ny = 2\nx - y\n", bytecode.OpSUB},
		{"mul", "x = 1\ny = 2\nx * y\n", bytecode.OpMUL},
		{"div", "x = 1\ny = 2\nx / y\n", bytecode.OpDIV},
		{"eq", "x = 1\ny = 2\nx == y\n", bytecode.OpEQ},
		{"lt", "x = 1\ny = 2\nx < y\n", bytecode.OpLT},
		{"le", "x = 1\ny = 2\nx <= y\n", bytecode.OpLE},
		{"gt", "x = 1\ny = 2\nx > y\n", bytecode.OpGT},
		{"ge", "x = 1\ny = 2\nx >= y\n", bytecode.OpGE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := compileInstructions(t, tt.src)
			if !hasOpcode(instructions, tt.want) {
				t.Fatalf("bytecode does not contain %s:\n%s", tt.want, disassemble(t, tt.src))
			}
			if hasOpcode(instructions, bytecode.OpSEND) || hasOpcode(instructions, bytecode.OpSSEND) {
				t.Fatalf("bytecode still contains a SEND:\n%s", disassemble(t, tt.src))
			}
		})
	}
}

func TestIntegerImmediateOpcodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bytecode.Opcode
		imm  uint32
	}{
		{"add one", "x = 5\nx + 1\n", bytecode.OpADDI, 1},
		{"add zero", "x = 5\nx + 0\n", bytecode.OpADDI, 0},
		{"add max", "x = 5\nx + 255\n", bytecode.OpADDI, 255},
		{"sub two", "x = 5\nx - 2\n", bytecode.OpSUBI, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := compileInstructions(t, tt.src)
			for _, in := range instructions {
				if in.Op == tt.want {
					if len(in.Operands) != 2 || in.Operands[1] != tt.imm {
						t.Fatalf("%s operands = %v, want immediate %d:\n%s",
							tt.want, in.Operands, tt.imm, disassemble(t, tt.src))
					}
					return
				}
			}
			t.Fatalf("bytecode does not contain %s:\n%s", tt.want, disassemble(t, tt.src))
		})
	}
}

func TestIntegerImmediateOutOfRangeStaysArithmetic(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bytecode.Opcode
	}{
		{"negative addend", "x = 5\nx + -1\n", bytecode.OpADD},
		{"negative subtrahend", "x = 5\nx - -1\n", bytecode.OpSUB},
		{"too large addend", "x = 5\nx + 256\n", bytecode.OpADD},
		{"too large subtrahend", "x = 5\nx - 256\n", bytecode.OpSUB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := compileInstructions(t, tt.src)
			if hasOpcode(instructions, bytecode.OpADDI) || hasOpcode(instructions, bytecode.OpSUBI) {
				t.Fatalf("out-of-range literal was folded:\n%s", disassemble(t, tt.src))
			}
			if !hasOpcode(instructions, tt.want) {
				t.Fatalf("bytecode does not contain %s:\n%s", tt.want, disassemble(t, tt.src))
			}
		})
	}
}

func TestLocalOpAssignOpcodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bytecode.Opcode
	}{
		{"add", "def bump(n)\n  x = n\n  x += 1\n  x\nend\n", bytecode.OpADDILV},
		{"sub", "def bump(n)\n  x = n\n  x -= 2\n  x\nend\n", bytecode.OpSUBILV},
		{"parameter", "def bump(x)\n  x += 1\n  x\nend\n", bytecode.OpADDILV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := allInstructions(t, compileIrep(t, tt.src))
			for _, in := range instructions {
				if in.Op == tt.want {
					return
				}
			}
			t.Fatalf("bytecode does not contain %s:\n%s", tt.want, disassemble(t, tt.src))
		})
	}
}

func TestLocalOpAssignNotFused(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"captured local", "x = 0\n[1].each { x += 1 }\n"},
		{"non literal", "def f(a, b)\n  a += b\n  a\nend\n"},
		{"negative literal", "def f(a)\n  a += -1\n  a\nend\n"},
		{"large literal", "def f(a)\n  a += 256\n  a\nend\n"},
		{"attribute", "class C\n  attr_accessor :value\nend\nc = C.new\nc.value = 0\nc.value += 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := allInstructions(t, compileIrep(t, tt.src))
			if hasOpcode(instructions, bytecode.OpADDILV) || hasOpcode(instructions, bytecode.OpSUBILV) {
				t.Fatalf("unexpected fused opcode:\n%s", disassemble(t, tt.src))
			}
		})
	}
}

func TestLocalOpAssignInBlock(t *testing.T) {
	src := "[1].each do\n  x = 0\n  x += 1\nend\n"
	instructions := allInstructions(t, compileIrep(t, src))
	if !hasOpcode(instructions, bytecode.OpADDILV) {
		t.Fatalf("block-local += was not fused:\n%s", disassemble(t, src))
	}
}

func TestSend0Opcodes(t *testing.T) {
	t.Run("explicit receiver", func(t *testing.T) {
		instructions := compileInstructions(t, "x = \"a\"\nx.upcase\n")
		if !hasOpcode(instructions, bytecode.OpSEND0) {
			t.Fatalf("bytecode does not contain SEND0:\n%s", disassemble(t, "x = \"a\"\nx.upcase\n"))
		}
	})
	t.Run("implicit self", func(t *testing.T) {
		instructions := compileInstructions(t, "puts\n")
		if !hasOpcode(instructions, bytecode.OpSSEND0) {
			t.Fatalf("bytecode does not contain SSEND0:\n%s", disassemble(t, "puts\n"))
		}
	})
}

func TestIndexOpcodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bytecode.Opcode
	}{
		{"zero index", "a = [1, 2]\na[0]\n", bytecode.OpGETIDX0},
		{"variable index", "a = [1, 2]\ni = 1\na[i]\n", bytecode.OpGETIDX},
		{"index write", "a = [1, 2]\ni = 1\na[i] = 3\n", bytecode.OpSETIDX},
		{"hash write", "h = {}\nh[:x] = 1\n", bytecode.OpSETIDX},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := compileInstructions(t, tt.src)
			if !hasOpcode(instructions, tt.want) {
				t.Fatalf("bytecode does not contain %s:\n%s", tt.want, disassemble(t, tt.src))
			}
		})
	}
}

func TestLocalOpAssignInstructionCount(t *testing.T) {
	src := "def bump(n)\n  x = n\n  x += 1\n  x - 2\nend\n"
	irep := compileIrep(t, src)
	if len(irep.Reps) != 1 {
		t.Fatalf("bump irep count = %d, want 1", len(irep.Reps))
	}
	instructions := decodeCode(t, irep.Reps[0].Code)
	if got := countOpcode(instructions, bytecode.OpMOVE); got > 2 {
		t.Fatalf("bump has %d MOVEs, want at most 2:\n%s", got, disassemble(t, src))
	}
	for _, op := range []bytecode.Opcode{
		bytecode.OpADDILV, bytecode.OpSUBI, bytecode.OpSEND, bytecode.OpSSEND,
	} {
		if op == bytecode.OpADDILV || op == bytecode.OpSUBI {
			if !hasOpcode(instructions, op) {
				t.Fatalf("bump does not contain %s:\n%s", op, disassemble(t, src))
			}
			continue
		}
		if hasOpcode(instructions, op) {
			t.Fatalf("bump still contains %s:\n%s", op, disassemble(t, src))
		}
	}
}

func TestNestedCallRegisters(t *testing.T) {
	src := "def fib(n)\n  if n < 2\n    n\n  else\n    fib(n - 1) + fib(n - 2)\n  end\nend\n"
	irep := compileIrep(t, src)
	if len(irep.Reps) != 1 {
		t.Fatalf("fib irep count = %d, want 1", len(irep.Reps))
	}
	instructions := decodeCode(t, irep.Reps[0].Code)
	if got := countOpcode(instructions, bytecode.OpMOVE); got > 4 {
		t.Fatalf("fib has %d MOVEs, want at most 4:\n%s", got, disassemble(t, src))
	}
	if !hasOpcode(instructions, bytecode.OpSUBI) || !hasOpcode(instructions, bytecode.OpADD) {
		t.Fatalf("fib does not use SUBI and ADD:\n%s", disassemble(t, src))
	}
	if hasOpcode(instructions, bytecode.OpSEND) {
		t.Fatalf("fib still contains a SEND:\n%s", disassemble(t, src))
	}
}

func TestOperatorOpcodeRegisterLimit(t *testing.T) {
	var top strings.Builder
	var method strings.Builder
	method.WriteString("def big\n")
	for i := 0; i < 260; i++ {
		fmt.Fprintf(&top, "v%d = %d\n", i, i)
		fmt.Fprintf(&method, "  v%d = %d\n", i, i)
	}
	top.WriteString("v0 + v1\n")
	method.WriteString("  v259 += 1\n  v259\nend\nbig\n")

	t.Run("add", func(t *testing.T) {
		instructions := compileInstructions(t, top.String())
		if hasOpcode(instructions, bytecode.OpADD) || hasOpcode(instructions, bytecode.OpADDI) {
			t.Fatalf("operator with register > 255 was encoded as an opcode")
		}
		if !hasOpcode(instructions, bytecode.OpSEND) {
			t.Fatalf("operator with register > 255 did not fall back to SEND")
		}
	})

	t.Run("local op assign", func(t *testing.T) {
		instructions := allInstructions(t, compileIrep(t, method.String()))
		if hasOpcode(instructions, bytecode.OpADDILV) {
			t.Fatalf("x += 1 with slot > 255 was fused")
		}
	})
}

func disassemble(t *testing.T, src string) string {
	t.Helper()
	irep, err := mrbc.Compile(context.Background(), []byte(src), mrbc.Options{FilePath: "ops.rb"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var b strings.Builder
	if err := bytecode.Disassemble(&b, irep); err != nil {
		t.Fatalf("Disassemble: %v", err)
	}
	return b.String()
}
