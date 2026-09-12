package accounts

import (
	"strings"
	"testing"
)

// An index name has to fit MySQL's 64-character identifier limit, which is
// the smallest of the three engines. A long table prefix used to produce a
// name that only one engine rejected.
func TestIndexName_FitsTheShortestIdentifierLimit(t *testing.T) {
	short := &SQLStore{prefix: "nucleus_"}
	if got := short.indexName("tokens_account"); got != "idx_nucleus_tokens_account" {
		t.Fatalf("a short prefix produced %q", got)
	}

	long := &SQLStore{prefix: strings.Repeat("very_long_prefix_", 5)}
	got := long.indexName("tokens_account")
	if len(got) > maxIndexNameLength {
		t.Fatalf("the name is %d characters: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "tokens_account") {
		t.Fatalf("truncation dropped the distinguishing part: %q", got)
	}
}
