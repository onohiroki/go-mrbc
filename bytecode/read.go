package bytecode

import (
	"encoding/binary"
	"fmt"
	"math"
)

type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int {
	return len(r.data) - r.pos
}

func (r *reader) need(n int) error {
	if n < 0 || r.remaining() < n {
		return fmt.Errorf("bytecode: truncated data at offset %d (need %d, have %d)", r.pos, n, r.remaining())
	}
	return nil
}

func (r *reader) readByte() (byte, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *reader) readUint16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *reader) readUint32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *reader) readBytes(n int) ([]byte, error) {
	if err := r.need(n); err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}

// ReadBinary parses a RITE 04.00 binary and returns its top-level irep.
// Optional sections are skipped. Only the first irep record of the IREP
// section is returned.
func ReadBinary(data []byte) (*Irep, error) {
	r := &reader{data: data}
	if err := r.need(binaryHeaderSize); err != nil {
		return nil, err
	}
	if string(data[0:4]) != binaryIdentifier {
		return nil, fmt.Errorf("bytecode: invalid binary identifier %q", data[0:4])
	}
	if string(data[4:6]) != binaryMajor || string(data[6:8]) != binaryMinor {
		return nil, fmt.Errorf("bytecode: unsupported RITE version %q.%q", data[4:6], data[6:8])
	}
	r.pos = binaryHeaderSize

	var top *Irep
	for {
		if err := r.need(sectionHeaderSize); err != nil {
			return nil, err
		}
		ident := string(r.data[r.pos : r.pos+4])
		r.pos += 4
		size, err := r.readUint32()
		if err != nil {
			return nil, err
		}
		if size < sectionHeaderSize {
			return nil, fmt.Errorf("bytecode: invalid section size %d for %q", size, ident)
		}
		payloadLen := int(size) - sectionHeaderSize
		if err := r.need(payloadLen); err != nil {
			return nil, fmt.Errorf("bytecode: section %q: %w", ident, err)
		}
		payload := r.data[r.pos : r.pos+payloadLen]
		r.pos += payloadLen

		switch ident {
		case sectionIrep:
			if len(payload) < 4 {
				return nil, fmt.Errorf("bytecode: IREP section too short")
			}
			if got := string(payload[0:4]); got != riteVersion {
				return nil, fmt.Errorf("bytecode: unsupported rite version %q", got)
			}
			section := &reader{data: payload, pos: 4}
			irep, err := readIrepRecord(section)
			if err != nil {
				return nil, err
			}
			if top == nil {
				top = irep
			}
		case sectionDebug:
			if top == nil {
				return nil, fmt.Errorf("bytecode: DBG section appears before IREP")
			}
			if err := readDebugSection(top, payload); err != nil {
				return nil, err
			}
		case sectionLV:
			if top == nil {
				return nil, fmt.Errorf("bytecode: LVAR section appears before IREP")
			}
			if err := readLVSection(top, payload); err != nil {
				return nil, err
			}
		case sectionEnd:
			if top == nil {
				return nil, fmt.Errorf("bytecode: no IREP section")
			}
			if err := Validate(top); err != nil {
				return nil, err
			}
			return top, nil
		default:
			// Unknown optional sections are ignored.
		}
	}
}

func readIrepRecord(r *reader) (*Irep, error) {
	recordStart := r.pos
	recordSize, err := r.readUint32()
	if err != nil {
		return nil, err
	}
	nlocals, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	nregs, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	childCount, err := r.readUint16()
	if err != nil {
		return nil, err
	}

	catchCount, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	codeLen, err := r.readUint32()
	if err != nil {
		return nil, err
	}
	if codeLen > math.MaxInt32 {
		return nil, fmt.Errorf("bytecode: instruction sequence too large: %d", codeLen)
	}
	code, err := r.readBytes(int(codeLen))
	if err != nil {
		return nil, err
	}

	irep := &Irep{
		Nlocals: nlocals,
		Nregs:   nregs,
		Code:    code,
	}
	if catchCount > 0 {
		irep.Catch = make([]CatchHandler, catchCount)
		for i := range irep.Catch {
			typ, err := r.readByte()
			if err != nil {
				return nil, err
			}
			begin, err := r.readUint32()
			if err != nil {
				return nil, err
			}
			end, err := r.readUint32()
			if err != nil {
				return nil, err
			}
			target, err := r.readUint32()
			if err != nil {
				return nil, err
			}
			irep.Catch[i] = CatchHandler{Type: typ, Begin: begin, End: end, Target: target}
		}
	}

	poolCount, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	if poolCount > 0 {
		irep.Pool = make([]PoolEntry, poolCount)
		for i := range irep.Pool {
			entry, err := readPoolEntry(r)
			if err != nil {
				return nil, err
			}
			irep.Pool[i] = entry
		}
	}

	symCount, err := r.readUint16()
	if err != nil {
		return nil, err
	}
	if symCount > 0 {
		irep.Syms = make([]string, symCount)
		for i := range irep.Syms {
			nameLen, err := r.readUint16()
			if err != nil {
				return nil, err
			}
			if nameLen == math.MaxUint16 {
				return nil, fmt.Errorf("bytecode: null symbols are not supported")
			}
			raw, err := r.readBytes(int(nameLen))
			if err != nil {
				return nil, err
			}
			terminator, err := r.readByte()
			if err != nil {
				return nil, err
			}
			if terminator != 0 {
				return nil, fmt.Errorf("bytecode: symbol %d is not NUL-terminated", i)
			}
			irep.Syms[i] = string(raw)
		}
	}

	consumed := r.pos - recordStart
	if uint32(consumed) != recordSize {
		return nil, fmt.Errorf("bytecode: irep record size mismatch: header says %d, read %d", recordSize, consumed)
	}

	for i := 0; i < int(childCount); i++ {
		child, err := readIrepRecord(r)
		if err != nil {
			return nil, err
		}
		irep.Reps = append(irep.Reps, child)
	}
	return irep, nil
}

func readPoolEntry(r *reader) (PoolEntry, error) {
	kind, err := r.readByte()
	if err != nil {
		return PoolEntry{}, err
	}
	switch kind {
	case 0, 2: // string / static string
		length, err := r.readUint16()
		if err != nil {
			return PoolEntry{}, err
		}
		raw, err := r.readBytes(int(length))
		if err != nil {
			return PoolEntry{}, err
		}
		terminator, err := r.readByte()
		if err != nil {
			return PoolEntry{}, err
		}
		if terminator != 0 {
			return PoolEntry{}, fmt.Errorf("bytecode: pool string is not NUL-terminated")
		}
		return PoolEntry{Kind: PoolString, String: string(raw)}, nil
	case 1:
		v, err := r.readUint32()
		if err != nil {
			return PoolEntry{}, err
		}
		return PoolEntry{Kind: PoolInt32, Int: int64(int32(v))}, nil
	case 3:
		hi, err := r.readUint32()
		if err != nil {
			return PoolEntry{}, err
		}
		lo, err := r.readUint32()
		if err != nil {
			return PoolEntry{}, err
		}
		return PoolEntry{Kind: PoolInt64, Int: int64(uint64(hi)<<32 | uint64(lo))}, nil
	case 5:
		raw, err := r.readBytes(8)
		if err != nil {
			return PoolEntry{}, err
		}
		bits := binary.LittleEndian.Uint64(raw)
		return PoolEntry{Kind: PoolFloat, Float: math.Float64frombits(bits)}, nil
	case 7:
		length, err := r.readByte()
		if err != nil {
			return PoolEntry{}, err
		}
		// The length byte counts the decimal digits. The payload that follows
		// is the sign byte plus those digits, so it is length+1 bytes.
		rest, err := r.readBytes(int(length) + 1)
		if err != nil {
			return PoolEntry{}, err
		}
		payload := make([]byte, 0, len(rest)+1)
		payload = append(payload, length)
		payload = append(payload, rest...)
		return PoolEntry{Kind: PoolBigInt, BigInt: payload}, nil
	default:
		return PoolEntry{}, fmt.Errorf("bytecode: unknown pool entry type %d", kind)
	}
}

// ReadInstructionSequence is a convenience wrapper around Decode for callers
// that only need the decoded instructions.
func ReadInstructionSequence(irep *Irep) ([]Instruction, error) {
	return Decode(irep.Code)
}

func readLVSection(top *Irep, payload []byte) error {
	r := &reader{data: payload}
	count, err := r.readUint32()
	if err != nil {
		return err
	}
	if count > math.MaxInt32 {
		return fmt.Errorf("bytecode: LVAR symbol count too large")
	}
	syms := make([]string, count)
	for i := range syms {
		length, err := r.readUint16()
		if err != nil {
			return err
		}
		raw, err := r.readBytes(int(length))
		if err != nil {
			return err
		}
		syms[i] = string(raw)
	}

	var ireps []*Irep
	walkIreps(top, func(irep *Irep) { ireps = append(ireps, irep) })
	for _, irep := range ireps {
		names := make([]string, irep.Nlocals)
		for slot := 1; slot < int(irep.Nlocals); slot++ {
			index, err := r.readUint16()
			if err != nil {
				return err
			}
			if index != math.MaxUint16 {
				if int(index) >= len(syms) {
					return fmt.Errorf("bytecode: LVAR symbol index %d out of range", index)
				}
				names[slot] = syms[index]
			}
		}
		irep.LocalNames = names
	}
	return nil
}

func readDebugSection(top *Irep, payload []byte) error {
	r := &reader{data: payload}
	filenameCount, err := r.readUint16()
	if err != nil {
		return err
	}
	filenames := make([]string, filenameCount)
	for i := range filenames {
		length, err := r.readUint16()
		if err != nil {
			return err
		}
		raw, err := r.readBytes(int(length))
		if err != nil {
			return err
		}
		filenames[i] = string(raw)
	}

	var ireps []*Irep
	walkIreps(top, func(irep *Irep) { ireps = append(ireps, irep) })
	for _, irep := range ireps {
		recordStart := r.pos
		recordSize, err := r.readUint32()
		if err != nil {
			return err
		}
		fileCount, err := r.readUint16()
		if err != nil {
			return err
		}
		var lines []DebugLine
		filename := ""
		for file := 0; file < int(fileCount); file++ {
			startPos, err := r.readUint32()
			if err != nil {
				return err
			}
			filenameIndex, err := r.readUint16()
			if err != nil {
				return err
			}
			if file == 0 && int(filenameIndex) < len(filenames) {
				filename = filenames[filenameIndex]
			}
			lineCount, err := r.readUint32()
			if err != nil {
				return err
			}
			lineType, err := r.readByte()
			if err != nil {
				return err
			}
			switch lineType {
			case 0: // mrb_debug_line_ary
				for i := uint32(0); i < lineCount; i++ {
					line, err := r.readUint16()
					if err != nil {
						return err
					}
					if line != 0 {
						lines = append(lines, DebugLine{Offset: int32(startPos + i), Line: int32(line)})
					}
				}
			case 1: // mrb_debug_line_flat_map
				for i := uint32(0); i < lineCount; i++ {
					pos, err := r.readUint32()
					if err != nil {
						return err
					}
					line, err := r.readUint16()
					if err != nil {
						return err
					}
					lines = append(lines, DebugLine{Offset: int32(pos), Line: int32(line)})
				}
			case 2: // mrb_debug_line_packed_map
				if lineCount > math.MaxInt32 {
					return fmt.Errorf("bytecode: packed debug map too large")
				}
				raw, err := r.readBytes(int(lineCount))
				if err != nil {
					return err
				}
				packed := &reader{data: raw}
				pos := uint32(0)
				line := uint32(0)
				for packed.remaining() > 0 {
					deltaPos, err := readPackedInt(packed)
					if err != nil {
						return err
					}
					deltaLine, err := readPackedInt(packed)
					if err != nil {
						return err
					}
					pos += deltaPos
					line += deltaLine
					lines = append(lines, DebugLine{Offset: int32(pos), Line: int32(line)})
				}
			default:
				return fmt.Errorf("bytecode: unknown debug line type %d", lineType)
			}
		}
		consumed := uint32(r.pos - recordStart)
		if consumed != recordSize {
			return fmt.Errorf("bytecode: debug record size mismatch: header says %d, read %d", recordSize, consumed)
		}
		irep.Filename = filename
		irep.Lines = lines
	}
	return nil
}

func readPackedInt(r *reader) (uint32, error) {
	var value uint32
	var shift uint
	for {
		b, err := r.readByte()
		if err != nil {
			return 0, err
		}
		value |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}
		shift += 7
		if shift >= 32 {
			return 0, fmt.Errorf("bytecode: packed integer too long")
		}
	}
}
