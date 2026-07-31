package suggest

import (
	"path/filepath"
	"testing"

	"github.com/wertyy111/metuur/internal/history"
)

func TestHistoryStoreRankingAndUnicode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.txt")
	store := history.Load(path, 100000)
	first := "metuur_test_проверить_проект"
	second := "metuur_test_другая_команда"
	store.Add(first)
	store.Add(second)
	store.Add(first)

	matches := store.Search("metuur_test_пп", 10)
	if len(matches) == 0 || matches[0].Command != first {
		t.Fatalf("unexpected fuzzy results: %#v", matches)
	}
	if matches[0].Frequency != 2 {
		t.Fatalf("frequency = %d, want 2", matches[0].Frequency)
	}
}
