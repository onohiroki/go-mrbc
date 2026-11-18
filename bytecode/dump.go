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

	sectionIrep = "IREP"
	sectionEnd  = "END\x00"

	binaryHeaderSize      = 20
	sectionHeaderSize     = 8
	irepSectionHeaderSize = 12
	irepHeaderSize        = 4 // the record-size field itself; nlocals/nregs/rlen follow
)

// DumpBinary serializes top as a RITE 04.00 binary that can be loaded by
// mruby 4.0.0.
func DumpBinary(top *Irep) ([]byte, error) {
	if top == nil {
		return nil, fmt.Errorf("bytecode: nil irep")
	}

	var body bytes.Buffer
	if err := appendIrepRecord(&body, top); err != nil {
		return nil, err
	}

	sectionSize := irepSectionHeaderSize + body.Len()
	totalSize := binaryHeaderSize + sectionSize + 8
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
	out = append(out, u32(uint32(sectionSize))...)
	out = append(out, riteVersion...)
	out = append(out, body.Bytes()...)

	out = append(out, sectionEnd...)
	out = append(out, u32(8)...)

	return out, nil
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
