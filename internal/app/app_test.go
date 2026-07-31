package app

import (
	"testing"

	"github.com/wertyy111/metuur/internal/suggest"
)

func TestAISuggestionIsRankedAndDeduplicated(t *testing.T) {
	ai := suggest.Suggestion{Insert: "go test ./...", Kind: "ai", Score: 550}
	items := []suggest.Suggestion{
		{Insert: "go build ./...", Kind: "build", Score: 600},
		{Insert: "GO TEST ./...", Kind: "workspace", Score: 700},
	}
	result := mergeAISuggestion(items, ai)
	if len(result) != 2 || result[0].Kind != "ai" || result[0].Score != 700 || result[1].Kind != "build" {
		t.Fatalf("unexpected merged suggestions: %#v", result)
	}
}
