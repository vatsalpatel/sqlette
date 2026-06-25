package values

import (
	"math"
	"strconv"
)

func Add(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return Value{Type: Null}
	}
	if a.Type == Integer && b.Type == Integer {
		return Value{Type: Integer, Int: a.Int + b.Int}
	}
	return NewReal(toReal(a) + toReal(b))
}

func Sub(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return Value{Type: Null}
	}
	if a.Type == Integer && b.Type == Integer {
		return Value{Type: Integer, Int: a.Int - b.Int}
	}
	return NewReal(toReal(a) - toReal(b))
}

func Mul(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return Value{Type: Null}
	}
	if a.Type == Integer && b.Type == Integer {
		return Value{Type: Integer, Int: a.Int * b.Int}
	}
	return NewReal(toReal(a) * toReal(b))
}

func Div(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return NewNull()
	}
	if (b.Type == Integer && b.Int == 0) || (b.Type == Real && b.Real == 0) {
		return NewNull()
	}
	if a.Type == Integer && b.Type == Integer {
		return Value{Type: Integer, Int: a.Int / b.Int}
	}
	return NewReal(toReal(a) / toReal(b))
}

func Mod(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return NewNull()
	}
	if (b.Type == Integer && b.Int == 0) || (b.Type == Real && b.Real == 0) {
		return NewNull()
	}
	if a.Type == Integer && b.Type == Integer {
		return Value{Type: Integer, Int: a.Int % b.Int}
	}
	return NewReal(math.Mod(toReal(a), toReal(b)))
}

func Concat(a, b Value) Value {
	if a.Type == Null || b.Type == Null {
		return Value{Type: Null}
	}
	if a.Type == Text && b.Type == Text {
		return Value{Type: Text, Text: a.Text + b.Text}
	}
	return NewText(toText(a) + toText(b))
}

func toReal(v Value) float64 {
	switch v.Type {
	case Integer:
		return float64(v.Int)
	case Real:
		return v.Real
	default:
		return 0
	}
}

func toText(v Value) string {
	switch v.Type {
	case Integer:
		return strconv.FormatInt(v.Int, 10)
	case Real:
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case Text:
		return v.Text
	case Blob:
		return string(v.Blob)
	default:
		return ""
	}
}
