package bytecode

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildPutsHello builds the equivalent of:
//
//	puts "hello"
//
// using the generic SSEND form so that no optimizer is required.
func buildPutsHello(t *testing.T) *Irep {
	t.Helper()
	var code []byte
	var err error
	emit := func(op Opcode, operands ...uint32) {
		code, err = EncodeInstruction(code, op, operands...)
		if err != nil {
			t.Fatalf("encode %s: %v", op, err)
		}
	}
	emit(OpLOADSYM, 1, 0)  // R1 = :puts
	emit(OpSTRING, 2, 0)   // R2 = "hello"
	emit(OpSSEND, 1, 0, 1) // self.puts(R2)
	emit(OpRETURN, 1)
	emit(OpSTOP)

	return &Irep{
		Nlocals: 1,
		Nregs:   3,
		Code:    code,
		Syms:    []string{"puts"},
		Pool:    []PoolEntry{StringEntry("hello")},
	}
}

func TestReadMrbcBinary(t *testing.T) {
	mrbc, err := exec.LookPath("mrbc")
	if err != nil {
		t.Skip("mrbc not found in PATH")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.rb")
	if err := os.WriteFile(src, []byte("x = 1\nputs x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "main.mrb")
	cmd := exec.Command(mrbc, "-o", out, src)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("mrbc failed (mruby 4.0.0 required): %v\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[4:8]) != "0400" {
		t.Skip("mrbc did not produce RITE 04.00")
	}
	irep, err := ReadBinary(data)
	if err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	if irep.Nlocals == 0 || len(irep.Code) == 0 {
		t.Fatalf("unexpected irep: %#v", irep)
	}
	var b bytes.Buffer
	if err := Disassemble(&b, irep); err != nil {
		t.Fatalf("Disassemble: %v", err)
	}
	if !strings.Contains(b.String(), "irep[0]") {
		t.Fatalf("unexpected disassembly:\n%s", b.String())
	}
}

func TestBinaryRunsOnMruby(t *testing.T) {
	mruby, err := exec.LookPath("mruby")
	if err != nil {
		t.Skip("mruby not found in PATH")
	}

	binary, err := DumpBinary(buildPutsHello(t))
	if err != nil {
		t.Fatalf("DumpBinary: %v", err)
	}

	path := filepath.Join(t.TempDir(), "hello.mrb")
	if err := os.WriteFile(path, binary, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(mruby, path)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mruby failed: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "hello"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
