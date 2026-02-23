package telegram

import (
	"sort"
	"strings"
	"unicode/utf8"
)

type telegramMessageEntity struct {
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Length   int    `json:"length"`
	Language string `json:"language,omitempty"`
}

type telegramTextChunk struct {
	Text     string
	Entities []telegramMessageEntity
}

func buildTelegramTextChunks(raw string, maxLen int) []telegramTextChunk {
	rendered, entities := parseMarkdownCodeEntities(raw)
	if len(entities) == 0 {
		plainChunks := splitText(rendered, maxLen)
		if len(plainChunks) == 0 {
			return []telegramTextChunk{{Text: ""}}
		}
		chunks := make([]telegramTextChunk, 0, len(plainChunks))
		for _, chunk := range plainChunks {
			chunks = append(chunks, telegramTextChunk{Text: chunk})
		}
		return chunks
	}
	return splitTextWithEntities(rendered, entities, maxLen)
}

func parseMarkdownCodeEntities(input string) (string, []telegramMessageEntity) {
	if !strings.Contains(input, "`") {
		return input, nil
	}

	var out strings.Builder
	entities := make([]telegramMessageEntity, 0, 2)
	utf16Pos := 0
	appendText := func(s string) {
		out.WriteString(s)
		utf16Pos += utf16Length(s)
	}

	for i := 0; i < len(input); {
		if strings.HasPrefix(input[i:], "```") {
			code, language, next, ok := parseFencedCodeSegment(input, i)
			if ok {
				codeLen := utf16Length(code)
				if codeLen > 0 {
					offset := utf16Pos
					appendText(code)
					entity := telegramMessageEntity{
						Type:   "pre",
						Offset: offset,
						Length: codeLen,
					}
					if language != "" {
						entity.Language = language
					}
					entities = append(entities, entity)
					i = next
					continue
				}
			}
		}

		if input[i] == '`' {
			closeRel := strings.IndexByte(input[i+1:], '`')
			if closeRel > 0 {
				code := input[i+1 : i+1+closeRel]
				if !strings.ContainsAny(code, "\r\n") {
					codeLen := utf16Length(code)
					if codeLen > 0 {
						offset := utf16Pos
						appendText(code)
						entities = append(entities, telegramMessageEntity{
							Type:   "code",
							Offset: offset,
							Length: codeLen,
						})
						i += closeRel + 2
						continue
					}
				}
			}
		}

		r, size := utf8.DecodeRuneInString(input[i:])
		if size == 0 {
			break
		}
		out.WriteString(input[i : i+size])
		utf16Pos += runeUTF16Len(r)
		i += size
	}

	return out.String(), entities
}

func parseFencedCodeSegment(input string, start int) (code string, language string, next int, ok bool) {
	if !strings.HasPrefix(input[start:], "```") {
		return "", "", 0, false
	}
	bodyStart := start + 3
	closeRel := strings.Index(input[bodyStart:], "```")
	if closeRel < 0 {
		return "", "", 0, false
	}

	body := input[bodyStart : bodyStart+closeRel]
	code, language = parseFenceBody(body)
	return code, language, bodyStart + closeRel + 3, true
}

func parseFenceBody(body string) (string, string) {
	line, rest, found := splitFirstLine(body)
	if !found {
		return body, ""
	}

	header := strings.TrimSpace(line)
	if header == "" {
		return rest, ""
	}
	if isFenceLanguageToken(header) {
		return rest, header
	}
	return body, ""
}

func splitFirstLine(s string) (line string, rest string, found bool) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			if i > 0 && s[i-1] == '\r' {
				return s[:i-1], s[i+1:], true
			}
			return s[:i], s[i+1:], true
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				return s[:i], s[i+2:], true
			}
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func isFenceLanguageToken(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '+', r == '-', r == '.', r == '#':
		default:
			return false
		}
	}
	return true
}

func splitTextWithEntities(text string, entities []telegramMessageEntity, maxLen int) []telegramTextChunk {
	if maxLen <= 0 {
		maxLen = maxTelegramTextLength
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return []telegramTextChunk{{Text: text}}
	}

	prefix := buildUTF16Prefix(runes)
	totalUTF16 := prefix[len(prefix)-1]
	if totalUTF16 <= maxLen {
		return []telegramTextChunk{{Text: text, Entities: entities}}
	}

	chunks := make([]telegramTextChunk, 0, totalUTF16/maxLen+1)
	for start := 0; start < totalUTF16; {
		target := start + maxLen
		if target > totalUTF16 {
			target = totalUTF16
		}

		end := floorUTF16Boundary(prefix, target)
		if end <= start {
			end = nextUTF16Boundary(prefix, start)
		}

		adjusted := moveSplitBeforeEntity(start, end, entities)
		if adjusted > start {
			end = adjusted
		}

		if end <= start {
			// Oversized entities can force a split inside the span.
			end = floorUTF16Boundary(prefix, target)
			if end <= start {
				end = nextUTF16Boundary(prefix, start)
			}
		}
		if end <= start {
			break
		}

		startRune := runeIndexForUTF16(prefix, start)
		endRune := runeIndexForUTF16(prefix, end)
		chunks = append(chunks, telegramTextChunk{
			Text:     string(runes[startRune:endRune]),
			Entities: sliceEntitiesForRange(entities, start, end),
		})
		start = end
	}

	if len(chunks) == 0 {
		return []telegramTextChunk{{Text: text, Entities: entities}}
	}
	return chunks
}

func moveSplitBeforeEntity(start, candidate int, entities []telegramMessageEntity) int {
	for {
		moved := false
		for _, e := range entities {
			eEnd := e.Offset + e.Length
			if candidate > e.Offset && candidate < eEnd {
				candidate = e.Offset
				moved = true
			}
		}
		if !moved || candidate <= start {
			return candidate
		}
	}
}

func sliceEntitiesForRange(entities []telegramMessageEntity, start, end int) []telegramMessageEntity {
	out := make([]telegramMessageEntity, 0, len(entities))
	for _, e := range entities {
		eStart := e.Offset
		eEnd := e.Offset + e.Length
		if eEnd <= start || eStart >= end {
			continue
		}

		partStart := maxInt(eStart, start)
		partEnd := minInt(eEnd, end)
		if partEnd <= partStart {
			continue
		}

		adj := e
		adj.Offset = partStart - start
		adj.Length = partEnd - partStart
		out = append(out, adj)
	}
	return out
}

func buildUTF16Prefix(runes []rune) []int {
	prefix := make([]int, len(runes)+1)
	for i, r := range runes {
		prefix[i+1] = prefix[i] + runeUTF16Len(r)
	}
	return prefix
}

func runeIndexForUTF16(prefix []int, offset int) int {
	idx := sort.Search(len(prefix), func(i int) bool {
		return prefix[i] >= offset
	})
	if idx >= len(prefix) {
		return len(prefix) - 1
	}
	return idx
}

func floorUTF16Boundary(prefix []int, target int) int {
	if target <= 0 {
		return 0
	}
	idx := sort.Search(len(prefix), func(i int) bool {
		return prefix[i] > target
	})
	if idx == 0 {
		return 0
	}
	return prefix[idx-1]
}

func nextUTF16Boundary(prefix []int, current int) int {
	idx := sort.Search(len(prefix), func(i int) bool {
		return prefix[i] > current
	})
	if idx >= len(prefix) {
		return prefix[len(prefix)-1]
	}
	return prefix[idx]
}

func runeUTF16Len(r rune) int {
	if r == utf8.RuneError {
		return 1
	}
	if r > 0xFFFF {
		return 2
	}
	return 1
}

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n += runeUTF16Len(r)
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
