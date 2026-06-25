package values_test

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func TestValueString(t *testing.T) {
	tests := []struct {
		name string
		val  values.Value
		want string
	}{
		{"null", values.NewNull(), "NULL"},
		{"integer", values.NewInteger(42), "42"},
		{"integer negative", values.NewInteger(-7), "-7"},
		{"integer zero", values.NewInteger(0), "0"},
		{"real", values.NewReal(3.14), "3.14"},
		{"real zero", values.NewReal(0), "0"},
		{"text", values.NewText("ada"), "'ada'"},
		{"text empty", values.NewText(""), "''"},
		{"text with spaces", values.NewText("ada lovelace"), "'ada lovelace'"},
		{"blob", values.NewBlob([]byte{0xab, 0xcd}), "X'abcd'"},
		{"blob empty", values.NewBlob([]byte{}), "X''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.val.String())
		})
	}
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name string
		got  values.Value
		want values.Value
	}{
		{"NewNull", values.NewNull(), values.Value{Type: values.Null}},
		{"NewInteger", values.NewInteger(42), values.Value{Type: values.Integer, Int: 42}},
		{"NewInteger negative", values.NewInteger(-7), values.Value{Type: values.Integer, Int: -7}},
		{"NewReal", values.NewReal(3.14), values.Value{Type: values.Real, Real: 3.14}},
		{"NewText", values.NewText("ada"), values.Value{Type: values.Text, Text: "ada"}},
		{"NewBlob", values.NewBlob([]byte{0x01, 0x02}), values.Value{Type: values.Blob, Blob: []byte{0x01, 0x02}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.want, tt.got)
		})
	}
}

func TestConstructorsSetOnlyTheirField(t *testing.T) {
	v := values.NewInteger(42)
	assert.Equal(t, float64(0), v.Real)
	assert.Equal(t, "", v.Text)
	assert.True(t, v.Blob == nil)

	v = values.NewText("x")
	assert.Equal(t, int64(0), v.Int)
	assert.Equal(t, float64(0), v.Real)
	assert.True(t, v.Blob == nil)
}

func TestStringPanicsOnUnknownType(t *testing.T) {
	defer func() {
		assert.True(t, recover() != nil)
	}()
	_ = values.Value{Type: 99}.String()
}
