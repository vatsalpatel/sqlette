package exec

import (
	"fmt"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
	"github.com/vatsalpatel/sqlette/internal/ast"
	"github.com/vatsalpatel/sqlette/internal/catalog"
	"github.com/vatsalpatel/sqlette/internal/storage"
	"github.com/vatsalpatel/sqlette/internal/token"
	"github.com/vatsalpatel/sqlette/internal/values"
)

var twoCol = &catalog.Table{
	Name:    "t",
	Columns: []catalog.Column{{Name: "a"}, {Name: "b"}},
}

func col(name string) *ast.ColumnRef { return &ast.ColumnRef{Name: name} }

func bin(op token.Kind, l, r ast.Expression) *ast.Binary {
	return &ast.Binary{Op: op, Left: l, Right: r}
}

func intLit(s string) *ast.Literal { return &ast.Literal{Kind: token.INT, Value: s} }

func mustEval(t *testing.T, expr ast.Expression, row storage.Row) values.Value {
	t.Helper()
	v, err := eval(expr, row, twoCol)
	assert.NoError(t, err)
	return v
}

func TestEvalColumnRef(t *testing.T) {
	row := storage.Row{values.NewText("ada"), values.NewInteger(36)}
	assert.DeepEqual(t, values.NewText("ada"), mustEval(t, col("a"), row))
	assert.DeepEqual(t, values.NewInteger(36), mustEval(t, col("b"), row))
}

func TestEvalLiteralComparison(t *testing.T) {
	row := storage.Row{values.NewInteger(36)}
	got := mustEval(t, bin(token.GT, col("a"), intLit("30")), row)
	assert.DeepEqual(t, values.NewInteger(1), got)
}

func TestEvalComparison(t *testing.T) {
	tests := []struct {
		name string
		op   token.Kind
		a, b values.Value
		want values.Value
	}{
		{"eq true", token.EQ, values.NewInteger(5), values.NewInteger(5), values.NewInteger(1)},
		{"eq false", token.EQ, values.NewInteger(5), values.NewInteger(6), values.NewInteger(0)},
		{"neq true", token.NEQ, values.NewInteger(5), values.NewInteger(6), values.NewInteger(1)},
		{"lt true", token.LT, values.NewInteger(5), values.NewInteger(6), values.NewInteger(1)},
		{"lte equal", token.LTE, values.NewInteger(5), values.NewInteger(5), values.NewInteger(1)},
		{"gt false", token.GT, values.NewInteger(5), values.NewInteger(6), values.NewInteger(0)},
		{"gte equal", token.GTE, values.NewInteger(5), values.NewInteger(5), values.NewInteger(1)},
		{"text eq", token.EQ, values.NewText("ada"), values.NewText("ada"), values.NewInteger(1)},
		{"text lt", token.LT, values.NewText("ada"), values.NewText("bob"), values.NewInteger(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := storage.Row{tt.a, tt.b}
			got := mustEval(t, bin(tt.op, col("a"), col("b")), row)
			assert.DeepEqual(t, tt.want, got)
		})
	}
}

// A comparison with a NULL operand is NULL, never true/false. This is the
// piece values.Compare can't express today (it ranks NULL below everything).
func TestEvalComparisonNullPropagates(t *testing.T) {
	ops := []token.Kind{token.EQ, token.NEQ, token.LT, token.LTE, token.GT, token.GTE}
	for _, op := range ops {
		t.Run(op.String(), func(t *testing.T) {
			row := storage.Row{values.NewNull(), values.NewInteger(5)}
			got := mustEval(t, bin(op, col("a"), col("b")), row)
			assert.DeepEqual(t, values.NewNull(), got)
		})
	}
}

func TestEvalAnd(t *testing.T) {
	T, F, N := values.NewInteger(1), values.NewInteger(0), values.NewNull()
	tests := []struct{ a, b, want values.Value }{
		{T, T, T}, {T, F, F}, {T, N, N},
		{F, T, F}, {F, F, F}, {F, N, F},
		{N, T, N}, {N, F, F}, {N, N, N},
	}
	for _, tt := range tests {
		fmt.Println(tt)
		t.Run(tt.a.String()+" AND "+tt.b.String(), func(t *testing.T) {
			row := storage.Row{tt.a, tt.b}
			got := mustEval(t, bin(token.AND, col("a"), col("b")), row)
			assert.DeepEqual(t, tt.want, got)
		})
	}
}

func TestEvalOr(t *testing.T) {
	T, F, N := values.NewInteger(1), values.NewInteger(0), values.NewNull()
	tests := []struct{ a, b, want values.Value }{
		{T, T, T}, {T, F, T}, {T, N, T},
		{F, T, T}, {F, F, F}, {F, N, N},
		{N, T, T}, {N, F, N}, {N, N, N},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+" OR "+tt.b.String(), func(t *testing.T) {
			row := storage.Row{tt.a, tt.b}
			got := mustEval(t, bin(token.OR, col("a"), col("b")), row)
			assert.DeepEqual(t, tt.want, got)
		})
	}
}

func TestEvalNot(t *testing.T) {
	tests := []struct{ in, want values.Value }{
		{values.NewInteger(1), values.NewInteger(0)},
		{values.NewInteger(0), values.NewInteger(1)},
		{values.NewNull(), values.NewNull()}, // NOT NULL = NULL
	}
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			row := storage.Row{tt.in}
			got := mustEval(t, &ast.Unary{Op: token.NOT, Operand: col("a")}, row)
			assert.DeepEqual(t, tt.want, got)
		})
	}
}
