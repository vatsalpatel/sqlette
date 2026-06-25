package values

import (
	"fmt"
	"strconv"
)

type Type uint8

const (
	Null Type = iota
	Integer
	Real
	Text
	Blob
)

type Value struct {
	Type Type
	Int  int64
	Real float64
	Text string
	Blob []byte
}

func (v Value) String() string {
	switch v.Type {
	case Null:
		return "NULL"
	case Integer:
		return strconv.FormatInt(v.Int, 10)
	case Real:
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case Text:
		return fmt.Sprintf("'%s'", v.Text)
	case Blob:
		return fmt.Sprintf("X'%x'", v.Blob)
	default:
		panic("unknown value type")
	}
}

func NewNull() Value {
	return Value{Type: Null}
}

func NewInteger(i int64) Value {
	return Value{Type: Integer, Int: i}
}

func NewReal(r float64) Value {
	return Value{Type: Real, Real: r}
}

func NewText(s string) Value {
	return Value{Type: Text, Text: s}
}

func NewBlob(b []byte) Value {
	return Value{Type: Blob, Blob: b}
}
