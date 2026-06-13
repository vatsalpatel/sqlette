package repl

import "testing"

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
					assertFalse(t, ready)
				}
			}
			assertEqual(t, tt.wantStmt, stmt)
			assertEqual(t, tt.wantReady, ready)
		})
	}
}

func TestScannerBackToBack(t *testing.T) {
	var s Scanner

	stmt, ready := s.Push("SELECT 1;")
	assertTrue(t, ready)
	assertEqual(t, "SELECT 1", stmt)

	stmt, ready = s.Push("SELECT 2;")
	assertTrue(t, ready)
	assertEqual(t, "SELECT 2", stmt)
}

func TestScannerPending(t *testing.T) {
	var s Scanner
	assertFalse(t, s.Pending())

	_, ready := s.Push("SELECT 1")
	assertFalse(t, ready)
	assertTrue(t, s.Pending())

	_, ready = s.Push("1;")
	assertTrue(t, ready)
	assertFalse(t, s.Pending())
}
