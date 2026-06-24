package repl

import (
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

func TestScannerPush(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		wantStmt  string
		wantReady bool
	}{
		{"single line", []string{"SELECT 1;"}, "SELECT 1", true},
		{"trailing spaces", []string{"SELECT 1 ;   "}, "SELECT 1", true},
		{"multi line", []string{"SELECT 1 +", "1;"}, "SELECT 1 +\n1", true},
		{"blank line first", []string{"", "SELECT 1;"}, "SELECT 1", true},
		{"incomplete", []string{"SELECT 1"}, "", false},
		{"lone semicolon", []string{";"}, "", true},
		{"semicolon in between", []string{"SELECT 1; SELECT 1;"}, "SELECT 1; SELECT 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Scanner
			var stmt string
			var ready bool
			for i, line := range tt.lines {
				stmt, ready = s.Push(line)
				if i < len(tt.lines)-1 {
					assert.False(t, ready)
				}
			}
			assert.Equal(t, tt.wantStmt, stmt)
			assert.Equal(t, tt.wantReady, ready)
		})
	}
}

func TestScannerBackToBack(t *testing.T) {
	var s Scanner

	stmt, ready := s.Push("SELECT 1;")
	assert.True(t, ready)
	assert.Equal(t, "SELECT 1", stmt)

	stmt, ready = s.Push("SELECT 2;")
	assert.True(t, ready)
	assert.Equal(t, "SELECT 2", stmt)
}

func TestScannerPending(t *testing.T) {
	var s Scanner
	assert.False(t, s.Pending())

	_, ready := s.Push("SELECT 1")
	assert.False(t, ready)
	assert.True(t, s.Pending())

	_, ready = s.Push("1;")
	assert.True(t, ready)
	assert.False(t, s.Pending())
}
