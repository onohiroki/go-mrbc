package bytecode

import "testing"

func TestValidate(t *testing.T) {
	valid := &Irep{
		Nlocals: 1,
		Nregs:   1,
		Code:    []byte{byte(OpSTOP)},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid irep rejected: %v", err)
	}

	if err := Validate(&Irep{Nlocals: 2, Nregs: 1, Code: []byte{byte(OpSTOP)}}); err == nil {
		t.Fatal("nlocals > nregs was accepted")
	}

	if err := Validate(&Irep{Nregs: 1, Code: []byte{byte(OpJMP), 0xff, 0xff}}); err == nil {
		t.Fatal("invalid jump target was accepted")
	}

	if err := Validate(&Irep{
		Nregs: 1,
		Code:  []byte{byte(OpSTOP)},
		Catch: []CatchHandler{{Begin: 1, End: 1, Target: 0}},
	}); err == nil {
		t.Fatal("empty catch range was accepted")
	}
}

func TestReadBinaryValidates(t *testing.T) {
	binary, err := DumpBinary(&Irep{Nlocals: 2, Nregs: 1, Code: []byte{byte(OpSTOP)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBinary(binary); err == nil {
		t.Fatal("ReadBinary accepted nlocals > nregs")
	}
}
