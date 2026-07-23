package token

import (
	"strings"
	"testing"

	"github.com/vatsalpatel/sqlette/internal/assert"
)

// Every keyword must have a kindNames entry, or Kind.String() falls back to
// "Kind(24)" and every parse error mentioning that token prints garbage —
// "expected Kind(26), got ...". Driving this off the keywords map means a
// keyword added without its kindNames entry fails here rather than silently
// degrading error messages.
func TestEveryKeywordKindHasAName(t *testing.T) {
	for word, kind := range keywords {
		t.Run(word, func(t *testing.T) {
			got := kind.String()
			if strings.HasPrefix(got, "Kind(") {
				t.Fatalf("keyword %q has no kindNames entry: String() = %s", word, got)
			}
			assert.Equal(t, word, got)
		})
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	tests := []struct {
		word string
		want Kind
	}{
		{"BEGIN", BEGIN}, {"begin", BEGIN}, {"BeGiN", BEGIN},
		{"COMMIT", COMMIT}, {"commit", COMMIT},
		{"ROLLBACK", ROLLBACK}, {"rollback", ROLLBACK},
		{"TRANSACTION", TRANSACTION}, {"transaction", TRANSACTION},
		{"END", END}, {"end", END},
	}
	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			assert.Equal(t, tt.want, Lookup(tt.word))
		})
	}
}

// END is a COMMIT synonym only at the parser level. The lexer must keep them
// distinct so the token stream mirrors the source text, and so END stays
// available for CASE ... END later.
func TestEndAndCommitAreDistinctKinds(t *testing.T) {
	assert.True(t, Lookup("END") != Lookup("COMMIT"))
	assert.Equal(t, "END", END.String())
	assert.Equal(t, "COMMIT", COMMIT.String())
}

func TestLookupLeavesNonKeywordsAlone(t *testing.T) {
	for _, word := range []string{"beginning", "committed", "rollbacks", "transactional", "ending"} {
		t.Run(word, func(t *testing.T) {
			assert.Equal(t, IDENT, Lookup(word))
		})
	}
}
