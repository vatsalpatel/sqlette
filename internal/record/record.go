package record

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/vatsalpatel/sqlette/internal/values"
)

func Encode(row []values.Value) []byte {
	buf := binary.AppendUvarint(nil, uint64(len(row)))
	for _, v := range row {
		switch v.Type {
		case values.Null:
			buf = binary.AppendUvarint(buf, 0)
		case values.Integer:
			buf = binary.AppendUvarint(buf, 1)
			buf = binary.BigEndian.AppendUint64(buf, uint64(v.Int))
		case values.Real:
			buf = binary.AppendUvarint(buf, 2)
			buf = binary.BigEndian.AppendUint64(buf, math.Float64bits(v.Real))
		case values.Text:
			buf = binary.AppendUvarint(buf, 3)
			buf = binary.AppendUvarint(buf, uint64(len(v.Text)))
			buf = append(buf, v.Text...)
		case values.Blob:
			buf = binary.AppendUvarint(buf, 4)
			buf = binary.AppendUvarint(buf, uint64(len(v.Blob)))
			buf = append(buf, v.Blob...)
		}
	}
	return buf
}

func Decode(b []byte) ([]values.Value, error) {
	if len(b) == 0 {
		return nil, nil
	}
	length, n := binary.Uvarint(b)
	if n <= 0 {
		return nil, fmt.Errorf("invalid record header: %v", b)
	}
	b = b[n:]
	row := make([]values.Value, 0, length)
	for range length {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, fmt.Errorf("invalid record tag: %v", b)
		}
		b = b[n:]
		switch tag {
		case 0:
			row = append(row, values.Value{Type: values.Null})
		case 1:
			if len(b) < 8 {
				return nil, fmt.Errorf("invalid integer: %v", b)
			}
			v := binary.BigEndian.Uint64(b)
			if n <= 0 {
				return nil, fmt.Errorf("invalid integer: %v", b)
			}
			b = b[8:]
			row = append(row, values.Value{Type: values.Integer, Int: int64(v)})
		case 2:
			if len(b) < 8 {
				return nil, fmt.Errorf("invalid real: %v", b)
			}
			v := binary.BigEndian.Uint64(b)
			if n <= 0 {
				return nil, fmt.Errorf("invalid real: %v", b)
			}
			b = b[8:]
			row = append(row, values.Value{Type: values.Real, Real: math.Float64frombits(v)})
		case 3:
			length, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, fmt.Errorf("invalid text length: %v", b)
			}
			b = b[n:]
			if len(b) < int(length) {
				return nil, fmt.Errorf("invalid text: %v", b)
			}
			row = append(row, values.Value{Type: values.Text, Text: string(bytes.Clone(b[:length]))})
			b = b[length:]
		case 4:
			length, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, fmt.Errorf("invalid blob length: %v", b)
			}
			b = b[n:]
			if len(b) < int(length) {
				return nil, fmt.Errorf("invalid blob: %v", b)
			}
			row = append(row, values.Value{Type: values.Blob, Blob: bytes.Clone(b[:length])})
			b = b[length:]
		default:
			return nil, fmt.Errorf("unknown record tag: %v", tag)
		}
	}
	return row, nil
}
