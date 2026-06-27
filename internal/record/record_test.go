package record_test

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/record"
	"github.com/vatsalpatel/sqlette/internal/values"
)

func valueEqual(a, b values.Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case values.Integer:
		return a.Int == b.Int
	case values.Real:
		return math.Float64bits(a.Real) == math.Float64bits(b.Real)
	case values.Text:
		return a.Text == b.Text
	case values.Blob:
		return bytes.Equal(a.Blob, b.Blob)
	default:
		return true
	}
}

func rowEqual(a, b []values.Value) bool {
	if len(a) != len(b) {
		fmt.Println("len", len(a), len(b))
		return false
	}
	for i := range a {
		if !valueEqual(a[i], b[i]) {
			fmt.Println(a[i], b[i])
			return false
		}
	}
	return true
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		row  []values.Value
	}{
		{"empty row", []values.Value{}},
		{"single null", []values.Value{values.NewNull()}},
		{"positive integer", []values.Value{values.NewInteger(1000)}},
		{"zero integer", []values.Value{values.NewInteger(0)}},
		{"negative integer", []values.Value{values.NewInteger(-1)}},
		{"int64 bounds", []values.Value{
			values.NewInteger(math.MinInt64),
			values.NewInteger(math.MaxInt64),
		}},
		{"reals", []values.Value{
			values.NewReal(3.14159),
			values.NewReal(0),
			values.NewReal(-2.5),
		}},
		{"special reals", []values.Value{
			values.NewReal(math.NaN()),
			values.NewReal(math.Inf(1)),
			values.NewReal(math.Inf(-1)),
			values.NewReal(math.Copysign(0, -1)),
		}},
		{"text", []values.Value{values.NewText("ada")}},
		{"empty text", []values.Value{values.NewText("")}},
		{"unicode text", []values.Value{values.NewText("héllo 世界")}},
		{"long text", []values.Value{values.NewText(strings.Repeat("a", 300))}},
		{"blob", []values.Value{values.NewBlob([]byte{0x00, 0x01, 0xff})}},
		{"empty blob", []values.Value{values.NewBlob([]byte{})}},
		{"long blob", []values.Value{values.NewBlob(bytes.Repeat([]byte{0xab}, 1000))}},
		{"mixed columns", []values.Value{
			values.NewInteger(1),
			values.NewText("ada"),
			values.NewNull(),
			values.NewReal(2.5),
			values.NewBlob([]byte{0xff, 0x00}),
		}},
		{"interleaved nulls", []values.Value{
			values.NewNull(),
			values.NewInteger(5),
			values.NewNull(),
			values.NewText("x"),
			values.NewNull(),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := record.Encode(tc.row)
			got, err := record.Decode(enc)
			assert.NoError(t, err)
			assert.True(t, rowEqual(tc.row, got))
		})
	}
}

func TestDecodeRejectsTruncatedInput(t *testing.T) {
	enc := record.Encode([]values.Value{
		values.NewInteger(1),
		values.NewText("ada"),
		values.NewReal(2.5),
		values.NewBlob([]byte{0xff, 0x00, 0x12}),
	})

	for n := 1; n < len(enc); n++ {
		_, err := record.Decode(enc[:n])
		if err == nil {
			t.Fatalf("want error decoding truncated input of length %d, got nil", n)
		}
	}
}

func TestDecodeBlobDoesNotAliasInput(t *testing.T) {
	orig := []byte{0x01, 0x02, 0x03, 0x04}
	enc := record.Encode([]values.Value{values.NewBlob(orig)})

	got, err := record.Decode(enc)
	assert.NoError(t, err)

	for i := range enc {
		enc[i] = 0xff
	}

	assert.Equal(t, 1, len(got))
	assert.True(t, bytes.Equal(got[0].Blob, orig))
}
