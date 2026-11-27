package bytecode

import "testing"

func FuzzReadBinary(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("RITE"))
	f.Add([]byte("RITE0400"))
	if binary, err := DumpBinary(&Irep{Nregs: 1, Code: []byte{byte(OpSTOP)}}); err == nil {
		f.Add(binary)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = ReadBinary(data)
	})
}
