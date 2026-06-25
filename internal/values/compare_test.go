package values_test

import (
	"sort"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func vi(n int64) values.Value   { return values.NewInteger(n) }
func vr(f float64) values.Value { return values.NewReal(f) }
func vt(s string) values.Value  { return values.NewText(s) }
func vb(b ...byte) values.Value { return values.NewBlob(b) }
func vnull() values.Value       { return values.NewNull() }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b values.Value
		want int
	}{
		{"text less", vt("a"), vt("b"), -1},
		{"text equal", vt("abc"), vt("abc"), 0},
		{"text greater", vt("abd"), vt("abc"), 1},
		{"text uppercase sorts before lowercase", vt("B"), vt("a"), -1},

		{"blob less", vb(0x01), vb(0x02), -1},
		{"blob equal", vb(0x01, 0x02), vb(0x01, 0x02), 0},
		{"blob greater", vb(0x02), vb(0x01), 1},

		{"null before int", vnull(), vi(0), -1},
		{"null before text", vnull(), vt(""), -1},
		{"int before text", vi(999), vt("a"), -1},
		{"real before text", vr(1e9), vt("a"), -1},
		{"text before blob", vt("zzz"), vb(0x00), -1},

		{"int boundary", vi(9007199254740993), vi(9007199254740992), 1}, // float64 loses precision and returns 0

		{"null equals null", vnull(), vnull(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sign(values.Compare(tt.a, tt.b)))
		})
	}
}

func TestCompareAntisymmetry(t *testing.T) {
	vals := []values.Value{
		vnull(), vi(-5), vi(0), vi(5), vr(2.5), vt("a"), vt("b"), vb(0x01),
	}
	for _, a := range vals {
		for _, b := range vals {
			if sign(values.Compare(a, b))+sign(values.Compare(b, a)) != 0 {
				t.Errorf("antisymmetry violated: %v vs %v", a, b)
			}
		}
	}
}

func TestCompareSorts(t *testing.T) {
	got := []values.Value{vt("apple"), vi(1), vb(0x01), vnull(), vr(2.5)}
	sort.Slice(got, func(i, j int) bool {
		return values.Compare(got[i], got[j]) < 0
	})
	want := []values.Value{vnull(), vi(1), vr(2.5), vt("apple"), vb(0x01)}
	assert.DeepEqual(t, want, got)
}

func TestCompareNumeric(t *testing.T) {
	tests := []struct {
		name string
		a, b values.Value
		want int
	}{
		{"int < int", vi(1), vi(2), -1},
		{"int = int", vi(2), vi(2), 0},
		{"int > int", vi(3), vi(2), 1},

		{"real < real", vr(1.5), vr(2.5), -1},
		{"real = real", vr(2.5), vr(2.5), 0},
		{"real > real", vr(3.5), vr(2.5), 1},

		{"int < real", vi(2), vr(2.5), -1},
		{"int = real", vi(2), vr(2.0), 0},
		{"int > real", vi(3), vr(2.5), 1},

		{"real < int", vr(1.5), vi(2), -1},
		{"real = int", vr(2.0), vi(2), 0},
		{"real > int", vr(2.5), vi(2), 1},

		{"negatives mixed", vi(-3), vr(-2.5), -1},
		{"zero vs negative real", vi(0), vr(-0.5), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sign(values.Compare(tt.a, tt.b)))
		})
	}
}
