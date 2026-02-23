package telegram

import (
	"strings"
	"testing"
)

func TestParseMarkdownCodeEntitiesMixed(t *testing.T) {
	t.Parallel()

	input := "run `go test ./...`\n```go\nfmt.Println(\"hi\")\n```\nnow"
	gotText, gotEntities := parseMarkdownCodeEntities(input)

	wantText := "run go test ./...\nfmt.Println(\"hi\")\n\nnow"
	if gotText != wantText {
		t.Fatalf("unexpected rendered text:\nwant: %q\ngot:  %q", wantText, gotText)
	}
	if len(gotEntities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(gotEntities))
	}
	if gotEntities[0].Type != "code" {
		t.Fatalf("first entity type=%q want=code", gotEntities[0].Type)
	}
	if span := entitySpan(gotText, gotEntities[0]); span != "go test ./..." {
		t.Fatalf("first entity span=%q want=%q", span, "go test ./...")
	}
	if gotEntities[1].Type != "pre" {
		t.Fatalf("second entity type=%q want=pre", gotEntities[1].Type)
	}
	if gotEntities[1].Language != "go" {
		t.Fatalf("second entity language=%q want=%q", gotEntities[1].Language, "go")
	}
	if span := entitySpan(gotText, gotEntities[1]); span != "fmt.Println(\"hi\")\n" {
		t.Fatalf("second entity span=%q want=%q", span, "fmt.Println(\"hi\")\n")
	}
}

func TestParseMarkdownCodeEntitiesUnmatchedFence(t *testing.T) {
	t.Parallel()

	input := "hello ```go\nfmt.Println(\"hi\")"
	gotText, gotEntities := parseMarkdownCodeEntities(input)
	if gotText != input {
		t.Fatalf("unexpected text for unmatched fence:\nwant: %q\ngot:  %q", input, gotText)
	}
	if len(gotEntities) != 0 {
		t.Fatalf("expected 0 entities for unmatched fence, got %d", len(gotEntities))
	}
}

func TestParseMarkdownCodeEntitiesUTF16Offsets(t *testing.T) {
	t.Parallel()

	input := "💡 `x` done"
	gotText, gotEntities := parseMarkdownCodeEntities(input)
	if gotText != "💡 x done" {
		t.Fatalf("unexpected rendered text: %q", gotText)
	}
	if len(gotEntities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(gotEntities))
	}
	if gotEntities[0].Offset != 3 {
		t.Fatalf("entity offset=%d want=3", gotEntities[0].Offset)
	}
	if gotEntities[0].Length != 1 {
		t.Fatalf("entity length=%d want=1", gotEntities[0].Length)
	}
	if span := entitySpan(gotText, gotEntities[0]); span != "x" {
		t.Fatalf("entity span=%q want=%q", span, "x")
	}
}

func TestSplitTextWithEntitiesKeepsSmallEntityWhole(t *testing.T) {
	t.Parallel()

	text := "abc defghij xyz"
	entities := []telegramMessageEntity{
		{Type: "code", Offset: 4, Length: 7},
	}
	chunks := splitTextWithEntities(text, entities, 8)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Text != "abc " {
		t.Fatalf("unexpected first chunk text: %q", chunks[0].Text)
	}
	if chunks[1].Text != "defghij " {
		t.Fatalf("unexpected second chunk text: %q", chunks[1].Text)
	}
	if len(chunks[1].Entities) != 1 {
		t.Fatalf("expected 1 entity in second chunk, got %d", len(chunks[1].Entities))
	}
	if chunks[1].Entities[0].Offset != 0 || chunks[1].Entities[0].Length != 7 {
		t.Fatalf("unexpected second chunk entity: %#v", chunks[1].Entities[0])
	}
	if chunks[2].Text != "xyz" {
		t.Fatalf("unexpected third chunk text: %q", chunks[2].Text)
	}
}

func TestSplitTextWithEntitiesSplitsOversizedEntity(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("a", 25)
	entities := []telegramMessageEntity{
		{Type: "pre", Offset: 0, Length: utf16Length(text)},
	}
	chunks := splitTextWithEntities(text, entities, 10)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	wantLengths := []int{10, 10, 5}
	for i, chunk := range chunks {
		gotLen := utf16Length(chunk.Text)
		if gotLen != wantLengths[i] {
			t.Fatalf("chunk %d utf16 length=%d want=%d", i, gotLen, wantLengths[i])
		}
		if len(chunk.Entities) != 1 {
			t.Fatalf("chunk %d expected 1 entity, got %d", i, len(chunk.Entities))
		}
		if chunk.Entities[0].Offset != 0 {
			t.Fatalf("chunk %d entity offset=%d want=0", i, chunk.Entities[0].Offset)
		}
		if chunk.Entities[0].Length != wantLengths[i] {
			t.Fatalf("chunk %d entity length=%d want=%d", i, chunk.Entities[0].Length, wantLengths[i])
		}
	}
}

func entitySpan(text string, entity telegramMessageEntity) string {
	runes := []rune(text)
	prefix := buildUTF16Prefix(runes)
	start := runeIndexForUTF16(prefix, entity.Offset)
	end := runeIndexForUTF16(prefix, entity.Offset+entity.Length)
	return string(runes[start:end])
}
