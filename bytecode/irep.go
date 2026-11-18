package bytecode

import "math"

// PoolKind identifies the type of a literal pool entry.
type PoolKind uint8

const (
	PoolString PoolKind = iota
	PoolInt32
	PoolInt64
	PoolFloat
	PoolBigInt
)

// PoolEntry is one entry in an irep literal pool.
//
// BigInt stores the raw mruby payload: a length byte, a sign byte, and the
// magnitude bytes. It is preserved verbatim so that binaries can round-trip
// even when the compiler does not construct big integers itself.
type PoolEntry struct {
	Kind   PoolKind
	String string
	Int    int64
	Float  float64
	BigInt []byte
}

// StringEntry returns a string pool entry.
func StringEntry(s string) PoolEntry {
	return PoolEntry{Kind: PoolString, String: s}
}

// IntEntry returns an integer pool entry, choosing int32 when the value fits.
func IntEntry(v int64) PoolEntry {
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		return PoolEntry{Kind: PoolInt32, Int: v}
	}
	return PoolEntry{Kind: PoolInt64, Int: v}
}

// FloatEntry returns a floating-point pool entry.
func FloatEntry(v float64) PoolEntry {
	return PoolEntry{Kind: PoolFloat, Float: v}
}

// CatchHandler describes a rescue or ensure handler.
type CatchHandler struct {
	Type   uint8
	Begin  uint32
	End    uint32
	Target uint32
}

const (
	CatchRescue uint8 = 0
	CatchEnsure uint8 = 1
)

// CatchHandlerSize is the encoded size of one catch handler.
const CatchHandlerSize = 13

// Irep is one mruby instruction representation: an instruction sequence and
// its associated literals, symbols, and child ireps.
type Irep struct {
	Nlocals uint16
	Nregs   uint16
	Code    []byte
	Catch   []CatchHandler
	Pool    []PoolEntry
	Syms    []string
	Reps    []*Irep
}

// AddChild appends a child irep and returns its index.
func (irep *Irep) AddChild(child *Irep) int {
	irep.Reps = append(irep.Reps, child)
	return len(irep.Reps) - 1
}

// SymbolIndex returns the index of name in the symbol table, adding it if it
// is not present. The empty string is a valid symbol.
func (irep *Irep) SymbolIndex(name string) int {
	for i, existing := range irep.Syms {
		if existing == name {
			return i
		}
	}
	irep.Syms = append(irep.Syms, name)
	return len(irep.Syms) - 1
}

// PoolIndex returns the index of entry in the literal pool, adding it if it is
// not present. String and float entries are compared by value; byte slices are
// not deduplicated.
func (irep *Irep) PoolIndex(entry PoolEntry) int {
	for i, existing := range irep.Pool {
		if existing.Kind != entry.Kind {
			continue
		}
		switch entry.Kind {
		case PoolString:
			if existing.String == entry.String {
				return i
			}
		case PoolInt32, PoolInt64:
			if existing.Int == entry.Int {
				return i
			}
		case PoolFloat:
			if existing.Float == entry.Float {
				return i
			}
		}
	}
	irep.Pool = append(irep.Pool, entry)
	return len(irep.Pool) - 1
}
