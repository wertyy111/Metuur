package app

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wertyy111/metuur/internal/suggest"
)

func TestNotifyingWriterSignalsChildOutputWithoutBlocking(t *testing.T) {
	var output bytes.Buffer
	events := make(chan struct{}, 1)
	writer := &notifyingWriter{writer: &output, events: events}

	for _, chunk := range []string{"go ", "bui"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if output.String() != "go bui" {
		t.Fatalf("child output = %q", output.String())
	}
	if len(events) != 1 {
		t.Fatalf("coalesced output events = %d, want 1", len(events))
	}
}

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

func TestInputDecoderRecognizesIRISKeysAndUTF8(t *testing.T) {
	var decoder inputDecoder
	data := append([]byte("go "), []byte("\x1b[Z\x1b[A\x1b[B\t\r")...)
	strokes := decoder.Feed(data)
	var kinds []strokeKind
	var text []rune
	for _, stroke := range strokes {
		kinds = append(kinds, stroke.kind)
		if stroke.kind == strokeRune {
			text = append(text, stroke.runeValue)
		}
	}
	wantKinds := []strokeKind{
		strokeRune, strokeRune, strokeRune,
		strokeShiftTab, strokeUp, strokeDown, strokeTab, strokeEnter,
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("decoded kinds = %#v, want %#v", kinds, wantKinds)
	}
	if string(text) != "go " {
		t.Fatalf("decoded text = %q", string(text))
	}
}

func TestInputDecoderKeepsSplitCyrillicRune(t *testing.T) {
	var decoder inputDecoder
	encoded := []byte("я")
	if got := decoder.Feed(encoded[:1]); len(got) != 0 {
		t.Fatalf("incomplete UTF-8 rune was emitted: %#v", got)
	}
	got := decoder.Feed(encoded[1:])
	if len(got) != 1 || got[0].kind != strokeRune || got[0].runeValue != 'я' {
		t.Fatalf("split UTF-8 rune decoded incorrectly: %#v", got)
	}
}

func TestInputDecoderKeepsSplitVTSequence(t *testing.T) {
	var decoder inputDecoder
	if got := decoder.Feed([]byte{0x1b}); len(got) != 0 || !decoder.HasPending() {
		t.Fatalf("standalone VT prefix must stay pending: %#v", got)
	}
	got := decoder.Feed([]byte("[A"))
	if len(got) != 1 || got[0].kind != strokeUp || decoder.HasPending() {
		t.Fatalf("split arrow decoded incorrectly: %#v", got)
	}

	if got := decoder.Feed([]byte{0x1b}); len(got) != 0 {
		t.Fatalf("Escape must wait for the ambiguity timeout: %#v", got)
	}
	got = decoder.FlushPending()
	if len(got) != 1 || got[0].kind != strokeEscape {
		t.Fatalf("timed-out Escape decoded incorrectly: %#v", got)
	}
}
