package bytecode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

const (
	binaryIdentifier = "RITE"
	binaryMajor      = "04"
	binaryMinor      = "00"
	compilerName     = "MATZ"
	compilerVersion  = "0000"

	riteVersion = "0400"

	sectionIrep  = "IREP"
	sectionDebug = "DBG\x00"
	sectionLV    = "LVAR"
	sectionEnd   = "END\x00"

	binaryHeaderSize      = 20
	sectionHeaderSize     = 8
	irepSectionHeaderSize = 12
	irepHeaderSize        = 4 // the record-size field itself; nlocals/nregs/rlen follow
)

// DumpOptions controls optional sections in a RITE binary.
type DumpOptions struct {
	// DebugInfo emits the DBG section with source line information.
	DebugInfo bool
	// RemoveLV omits the LVAR section even when local names are available.
	RemoveLV bool
}

// DumpBinary serializes top as a RITE 04.00 binary with default options.
func DumpBinary(top *Irep) ([]byte, error) {
	return DumpBinaryWithOptions(top, DumpOptions{})
}

// DumpBinaryWithOptions serializes top as a RITE 04.00 binary.
func DumpBinaryWithOptions(top *Irep, opts DumpOptions) ([]byte, error) {
	if top == nil {
		return nil, fmt.Errorf("bytecode: nil irep")
	}

	var irepBody bytes.Buffer
	if err := appendIrepRecord(&irepBody, top); err != nil {
		return nil, err
	}

	irepSectionSize := irepSectionHeaderSize + irepBody.Len()
	if irepSectionSize > math.MaxUint32 {
		return nil, fmt.Errorf("bytecode: IREP section too large: %d bytes", irepSectionSize)
	}

	var debugSection []byte
	if opts.DebugInfo {
		var err error
		debugSection, err = buildDebugSection(top)
		if err != nil {
			return nil, err
		}
	}
	var lvSection []byte
	if !opts.RemoveLV {
		var err error
		lvSection, err = buildLVSection(top)
		if err != nil {
			return nil, err
		}
	}

	totalSize := binaryHeaderSize + irepSectionSize + len(debugSection) + len(lvSection) + 8
	if totalSize > math.MaxUint32 {
		return nil, fmt.Errorf("bytecode: binary too large: %d bytes", totalSize)
	}

	out := make([]byte, 0, totalSize)
	out = append(out, binaryIdentifier...)
	out = append(out, binaryMajor...)
	out = append(out, binaryMinor...)
	out = append(out, u32(uint32(totalSize))...)
	out = append(out, compilerName...)
	out = append(out, compilerVersion...)

	out = append(out, sectionIrep...)
	out = append(out, u32(uint32(irepSectionSize))...)
	out = append(out, riteVersion...)
	out = append(out, irepBody.Bytes()...)

	out = append(out, debugSection...)
	out = append(out, lvSection...)

	out = append(out, sectionEnd...)
	out = append(out, u32(8)...)

	return out, nil
}

func walkIreps(top *Irep, fn func(*Irep)) {
	if top == nil {
		return
	}
	fn(top)
	for _, child := range top.Reps {
		walkIreps(child, fn)
	}
}

func buildLVSection(top *Irep) ([]byte, error) {
	var names []string
	index := make(map[string]int)
	walkIreps(top, func(irep *Irep) {
		for slot := 1; slot < int(irep.Nlocals) && slot < len(irep.LocalNames); slot++ {
			name := irep.LocalNames[slot]
			if name == "" {
				continue
			}
			if _, ok := index[name]; !ok {
				index[name] = len(names)
				names = append(names, name)
			}
		}
	})
	if len(names) == 0 {
		return nil, nil
	}

	var payload bytes.Buffer
	payload.Write(u32(uint32(len(names))))
	for _, name := range names {
		if len(name) > math.MaxUint16 {
			return nil, fmt.Errorf("bytecode: local variable name too long: %d bytes", len(name))
		}
		payload.Write(u16(uint16(len(name))))
		payload.WriteString(name)
	}
	walkIreps(top, func(irep *Irep) {
		for slot := 1; slot < int(irep.Nlocals); slot++ {
			if slot < len(irep.LocalNames) && irep.LocalNames[slot] != "" {
				payload.Write(u16(uint16(index[irep.LocalNames[slot]])))
			} else {
				payload.Write(u16(math.MaxUint16))
			}
		}
	})

	sectionSize := sectionHeaderSize + payload.Len()
	if sectionSize > math.MaxUint32 {
		return nil, fmt.Errorf("bytecode: LVAR section too large")
	}
	section := make([]byte, 0, sectionSize)
	section = append(section, sectionLV...)
	section = append(section, u32(uint32(sectionSize))...)
	section = append(section, payload.Bytes()...)
	return section, nil
}

func buildDebugSection(top *Irep) ([]byte, error) {
	var filenames []string
	index := make(map[string]int)
	walkIreps(top, func(irep *Irep) {
		if _, ok := index[irep.Filename]; !ok {
			index[irep.Filename] = len(filenames)
			filenames = append(filenames, irep.Filename)
		}
	})
	if len(filenames) > math.MaxUint16 {
		return nil, fmt.Errorf("bytecode: too many debug filenames")
	}

	var payload bytes.Buffer
	payload.Write(u16(uint16(len(filenames))))
	for _, name := range filenames {
		if len(name) > math.MaxUint16 {
			return nil, fmt.Errorf("bytecode: debug filename too long")
		}
		payload.Write(u16(uint16(len(name))))
		payload.WriteString(name)
	}

	var buildErr error
	walkIreps(top, func(irep *Irep) {
		if buildErr != nil {
			return
		}
		packed := packDebugLines(irep.Lines)
		recordSize := 4 + 2 + 4 + 2 + 4 + 1 + len(packed)
		if recordSize > math.MaxUint32 {
			buildErr = fmt.Errorf("bytecode: debug record too large")
			return
		}
		payload.Write(u32(uint32(recordSize)))
		payload.Write(u16(1)) // one debug file per irep
		payload.Write(u32(0)) // start_pos
		payload.Write(u16(uint16(index[irep.Filename])))
		payload.Write(u32(uint32(len(packed))))
		payload.WriteByte(2) // mrb_debug_line_packed_map
		payload.Write(packed)
	})
	if buildErr != nil {
		return nil, buildErr
	}

	sectionSize := sectionHeaderSize + payload.Len()
	if sectionSize > math.MaxUint32 {
		return nil, fmt.Errorf("bytecode: DBG section too large")
	}
	section := make([]byte, 0, sectionSize)
	section = append(section, sectionDebug...)
	section = append(section, u32(uint32(sectionSize))...)
	section = append(section, payload.Bytes()...)
	return section, nil
}

func packDebugLines(lines []DebugLine) []byte {
	sorted := append([]DebugLine(nil), lines...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Offset < sorted[j-1].Offset; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var out []byte
	prevPC := int64(0)
	prevLine := int64(0)
	wrote := false
	for _, line := range sorted {
		if line.Offset < 0 || line.Line <= 0 {
			continue
		}
		value := int64(line.Line)
		if value < prevLine {
			value = prevLine
		}
		if value == prevLine {
			continue
		}
		out = appendPackedInt(out, uint32(int64(line.Offset)-prevPC))
		out = appendPackedInt(out, uint32(value-prevLine))
		prevPC = int64(line.Offset)
		prevLine = value
		wrote = true
	}
	if !wrote {
		out = appendPackedInt(out, 0)
		out = appendPackedInt(out, 1)
	}
	return out
}

func appendPackedInt(dst []byte, value uint32) []byte {
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if value == 0 {
			return dst
		}
	}
}

// u16 returns a big-endian uint16.
func u16(v uint16) []byte {
	return []byte{byte(v >> 8), byte(v)}
}

// u32 returns a big-endian uint32.
func u32(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func appendIrepRecord(dst *bytes.Buffer, irep *Irep) error {
	if len(irep.Reps) > math.MaxUint16 {
		return fmt.Errorf("bytecode: too many child ireps: %d", len(irep.Reps))
	}
	if len(irep.Code) > math.MaxUint32 {
		return fmt.Errorf("bytecode: instruction sequence too large: %d bytes", len(irep.Code))
	}
	if len(irep.Pool) > math.MaxUint16 {
		return fmt.Errorf("bytecode: too many pool entries: %d", len(irep.Pool))
	}
	if len(irep.Syms) > math.MaxUint16 {
		return fmt.Errorf("bytecode: too many symbols: %d", len(irep.Syms))
	}

	var own bytes.Buffer
	own.Write(u16(irep.Nlocals))
	own.Write(u16(irep.Nregs))
	own.Write(u16(uint16(len(irep.Reps))))

	own.Write(u16(uint16(len(irep.Catch))))
	own.Write(u32(uint32(len(irep.Code))))
	own.Write(irep.Code)
	for _, handler := range irep.Catch {
		own.WriteByte(handler.Type)
		own.Write(u32(handler.Begin))
		own.Write(u32(handler.End))
		own.Write(u32(handler.Target))
	}

	own.Write(u16(uint16(len(irep.Pool))))
	for _, entry := range irep.Pool {
		if err := appendPoolEntry(&own, entry); err != nil {
			return err
		}
	}

	own.Write(u16(uint16(len(irep.Syms))))
	for _, sym := range irep.Syms {
		if len(sym) > math.MaxUint16 {
			return fmt.Errorf("bytecode: symbol too long: %d bytes", len(sym))
		}
		own.Write(u16(uint16(len(sym))))
		own.WriteString(sym)
		own.WriteByte(0)
	}

	recordSize := irepHeaderSize + own.Len()
	if recordSize > math.MaxUint32 {
		return fmt.Errorf("bytecode: irep record too large: %d bytes", recordSize)
	}
	dst.Write(u32(uint32(recordSize)))
	dst.Write(own.Bytes())

	for _, child := range irep.Reps {
		if child == nil {
			return fmt.Errorf("bytecode: nil child irep")
		}
		if err := appendIrepRecord(dst, child); err != nil {
			return err
		}
	}
	return nil
}

func appendPoolEntry(dst *bytes.Buffer, entry PoolEntry) error {
	switch entry.Kind {
	case PoolString:
		if len(entry.String) > math.MaxUint16 {
			return fmt.Errorf("bytecode: pool string too long: %d bytes", len(entry.String))
		}
		dst.WriteByte(0)
		dst.Write(u16(uint16(len(entry.String))))
		dst.WriteString(entry.String)
		dst.WriteByte(0)
	case PoolInt32:
		if entry.Int < math.MinInt32 || entry.Int > math.MaxInt32 {
			return fmt.Errorf("bytecode: int32 pool entry out of range: %d", entry.Int)
		}
		dst.WriteByte(1)
		dst.Write(u32(uint32(int32(entry.Int))))
	case PoolInt64:
		value := uint64(entry.Int)
		dst.WriteByte(3)
		dst.Write(u32(uint32(value >> 32)))
		dst.Write(u32(uint32(value)))
	case PoolFloat:
		dst.WriteByte(5)
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], math.Float64bits(entry.Float))
		dst.Write(raw[:])
	case PoolBigInt:
		if len(entry.BigInt) < 2 {
			return fmt.Errorf("bytecode: malformed bigint pool entry")
		}
		if int(entry.BigInt[0]) != len(entry.BigInt)-2 {
			return fmt.Errorf("bytecode: bigint length mismatch")
		}
		dst.WriteByte(7)
		dst.Write(entry.BigInt)
	default:
		return fmt.Errorf("bytecode: unknown pool kind %d", entry.Kind)
	}
	return nil
}
