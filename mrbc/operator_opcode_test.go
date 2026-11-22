package mrbc_test

import (
	"context"
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
