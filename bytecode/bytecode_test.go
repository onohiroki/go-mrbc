package bytecode

import (
	"bytes"
	"reflect"
	"testing"
)

func TestOpcodeTable(t *testing.T) {
	if OpcodeCount != 119 {
		t.Fatalf("OpcodeCount = %d, want 119", OpcodeCount)
	}
	if OpNOP.String() != "NOP" {
		t.Errorf("OpNOP.String() = %q", OpNOP.String())
	}
	if OpSTOP.String() != "STOP" {
		t.Errorf("OpSTOP.String() = %q", OpSTOP.String())
	}
	if !OpSTOP.Valid() {
		t.Error("OpSTOP should be valid")
	}
	if Opcode(OpcodeCount).Valid() {
		t.Error("out-of-range opcode should not be valid")
	}
	if got := OpSEND.OperandKind(); got != OperandBBB {
		t.Errorf("OpSEND kind = %d, want OperandBBB", got)
	}
	if got := OpJMP.OperandKind(); got != OperandS {
		t.Errorf("OpJMP kind = %d, want OperandS", got)
	}
}

func TestEncodeDecode(t *testing.T) {
	tests := []struct {
		name     string
		op       Opcode
		operands []uint32
		want     []byte
	}{
		{
			name:     "loadi8",
			op:       OpLOADI8,
			operands: []uint32{1, 3},
			want:     []byte{0x03, 0x01, 0x03},
		},
		{
			name:     "loadi_3",
			op:       OpLOADI_3,
			operands: []uint32{1},
			want:     []byte{0x09, 0x01},
		},
		{
			name:     "send",
			op:       OpSEND,
			operands: []uint32{2, 0, 1},
			want:     []byte{0x32, 0x02, 0x00, 0x01},
		},
		{
			name:     "jmp",
			op:       OpJMP,
			operands: []uint32{0x0005},
			want:     []byte{0x26, 0x00, 0x05},
		},
		{
			name:     "ext1",
			op:       OpLOADSYM,
			operands: []uint32{300, 1},
			want:     []byte{0x73, 0x10, 0x01, 0x2c, 0x01},
		},
		{
			name:     "ext2",
			op:       OpLOADSYM,
			operands: []uint32{1, 300},
			want:     []byte{0x74, 0x10, 0x01, 0x01, 0x2c},
		},
		{
			name:     "ext3",
			op:       OpLOADSYM,
			operands: []uint32{300, 301},
			want:     []byte{0x75, 0x10, 0x01, 0x2c, 0x01, 0x2d},
		},
		{
			name:     "enter",
			op:       OpENTER,
			operands: []uint32{0x042000},
			want:     []byte{0x39, 0x04, 0x20, 0x00},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeInstruction(nil, tt.op, tt.operands...)
			if err != nil {
				t.Fatalf("EncodeInstruction: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("encoded = % x, want % x", got, tt.want)
			}

			in, err := DecodeInstruction(got, 0)
			if err != nil {
				t.Fatalf("DecodeInstruction: %v", err)
			}
			if in.Op != tt.op {
				t.Errorf("decoded op = %s, want %s", in.Op, tt.op)
			}
			if !reflect.DeepEqual(in.Operands, tt.operands) {
				t.Errorf("decoded operands = %v, want %v", in.Operands, tt.operands)
			}
			if in.Size != len(got) {
				t.Errorf("decoded size = %d, want %d", in.Size, len(got))
			}
		})
	}
}

func TestEncodeRejectsBadOperands(t *testing.T) {
	if _, err := EncodeInstruction(nil, OpMOVE, 1); err == nil {
		t.Error("expected operand count error")
	}
	if _, err := EncodeInstruction(nil, OpMOVE, 1, 1<<20); err == nil {
		t.Error("expected operand width error")
	}
	if _, err := EncodeInstruction(nil, Opcode(250), 1); err == nil {
		t.Error("expected unknown opcode error")
	}
}

func TestDecodeAllAndJumps(t *testing.T) {
	code := []byte{
		0x06, 0x01, // LOADI_0 R1
		0x28, 0x01, 0x00, 0x00, // JMPNOT R1 +0
		0x76, // STOP
	}
	instructions, err := Decode(code)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(instructions) != 3 {
		t.Fatalf("got %d instructions, want 3", len(instructions))
	}
	target, err := JumpTarget(instructions[1])
	if err != nil {
		t.Fatalf("JumpTarget: %v", err)
	}
	if target != 6 {
		t.Errorf("jump target = %d, want 6", target)
	}
}

func TestDumpReadRoundTrip(t *testing.T) {
	code, err := EncodeInstruction(nil, OpLOADI8, 1, 42)
	if err != nil {
		t.Fatal(err)
	}
	code, err = EncodeInstruction(code, OpRETURN, 1)
	if err != nil {
		t.Fatal(err)
	}
	top := &Irep{
		Nlocals: 2,
		Nregs:   2,
		Code:    code,
		Syms:    []string{"puts"},
		Pool: []PoolEntry{
			StringEntry("hello"),
			IntEntry(1234567890123),
			FloatEntry(1.5),
		},
	}

	binary, err := DumpBinary(top)
	if err != nil {
		t.Fatalf("DumpBinary: %v", err)
	}
	if string(binary[:4]) != "RITE" {
		t.Fatalf("missing RITE header: % x", binary[:4])
	}
	if string(binary[len(binary)-8:len(binary)-4]) != "END\x00" {
		t.Fatalf("missing END footer: % x", binary[len(binary)-8:])
	}

	got, err := ReadBinary(binary)
	if err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	if !reflect.DeepEqual(got, top) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, top)
	}
}

func TestDumpReadNested(t *testing.T) {
	childCode, _ := EncodeInstruction(nil, OpRETNIL)
	child := &Irep{Nlocals: 1, Nregs: 1, Code: childCode}
	topCode, _ := EncodeInstruction(nil, OpBLOCK, 1, 0)
	topCode, _ = EncodeInstruction(topCode, OpRETURN, 1)
	top := &Irep{Nlocals: 2, Nregs: 2, Code: topCode, Reps: []*Irep{child}}

	binary, err := DumpBinary(top)
	if err != nil {
		t.Fatalf("DumpBinary: %v", err)
	}
	got, err := ReadBinary(binary)
	if err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	if !reflect.DeepEqual(got, top) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, top)
	}
}

func TestCatchHandlerRoundTrip(t *testing.T) {
	code, _ := EncodeInstruction(nil, OpRETNIL)
	top := &Irep{
		Nlocals: 1,
		Nregs:   1,
		Code:    code,
		Catch: []CatchHandler{
			{Type: CatchRescue, Begin: 0, End: 2, Target: 2},
		},
	}
	binary, err := DumpBinary(top)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadBinary(binary)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Catch, top.Catch) {
		t.Fatalf("catch mismatch: %#v != %#v", got.Catch, top.Catch)
	}
}
