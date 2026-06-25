package values_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/values"
)

type arithCase struct {
	name string
	a, b values.Value
	want values.Value
}

func TestAdd(t *testing.T) {
	tests := []arithCase{
		{"int + int", vi(2), vi(3), vi(5)},
		{"int + real", vi(2), vr(0.5), vr(2.5)},
		{"real + real", vr(1.5), vr(2.5), vr(4.0)},
		{"null + int", vnull(), vi(3), vnull()},
		{"int + null", vi(3), vnull(), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Add(tt.a, tt.b))
		})
	}
}

func TestSub(t *testing.T) {
	tests := []arithCase{
		{"int - int", vi(5), vi(3), vi(2)},
		{"real - int", vr(5.5), vi(2), vr(3.5)},
		{"null - int", vnull(), vi(1), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Sub(tt.a, tt.b))
		})
	}
}

func TestMul(t *testing.T) {
	tests := []arithCase{
		{"int * int", vi(3), vi(4), vi(12)},
		{"int * real", vi(2), vr(1.5), vr(3.0)},
		{"int * null", vi(2), vnull(), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Mul(tt.a, tt.b))
		})
	}
}

func TestDiv(t *testing.T) {
	tests := []arithCase{
		{"int / int truncates", vi(7), vi(2), vi(3)},
		{"int / int negative", vi(-7), vi(2), vi(-3)},
		{"real / int", vr(7), vi(2), vr(3.5)},
		{"int / real", vi(7), vr(2), vr(3.5)},
		{"int div by zero", vi(1), vi(0), vnull()},
		{"real div by zero", vr(1), vr(0), vnull()},
		{"null / int", vnull(), vi(2), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Div(tt.a, tt.b))
		})
	}
}

func TestMod(t *testing.T) {
	tests := []arithCase{
		{"int % int", vi(7), vi(3), vi(1)},
		{"int % int negative", vi(-7), vi(3), vi(-1)},
		{"real operand makes result real", vr(7), vi(2), vr(1)},
		{"mod by zero", vi(1), vi(0), vnull()},
		{"null % int", vnull(), vi(3), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Mod(tt.a, tt.b))
		})
	}
}

func TestConcat(t *testing.T) {
	tests := []arithCase{
		{"text || text", vt("ab"), vt("cd"), vt("abcd")},
		{"text || int", vt("a"), vi(1), vt("a1")},
		{"int || text", vi(2), vt("b"), vt("2b")},
		{"null || text", vnull(), vt("a"), vnull()},
		{"text || null", vt("a"), vnull(), vnull()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, values.Concat(tt.a, tt.b))
		})
	}
}
