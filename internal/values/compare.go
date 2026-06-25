package values

import (
	"bytes"
	"strings"
)

func Compare(a, b Value) int {
	ra, rb := rank(a.Type), rank(b.Type)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	switch a.Type {
	case Null:
		return 0
	case Integer:
		return compareNumeric(a, b)
	case Real:
		return compareNumeric(a, b)
	case Text:
		return strings.Compare(a.Text, b.Text)
	case Blob:
		return bytes.Compare(a.Blob, b.Blob)
	default:
		panic("unknown value type")
	}
}

func compareNumeric(a, b Value) int {
	if a.Type == Integer && b.Type == Integer {
		if a.Int < b.Int {
			return -1
		} else if a.Int > b.Int {
			return 1
		}
		return 0
	}
	ra, rb := toReal(a), toReal(b)
	if ra < rb {
		return -1
	} else if ra > rb {
		return 1
	}
	return 0
}

func rank(t Type) int {
	switch t {
	case Null:
		return 0
	case Integer, Real:
		return 1
	case Text:
		return 2
	case Blob:
		return 3
	default:
		panic("unknown value type")
	}
}
