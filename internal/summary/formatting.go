package summary

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const summaryHashtag = "#dailydramadigest #срач"

type formatData struct {
	Version int          `json:"version"`
	Items   []formatItem `json:"items"`
}

type formatItem struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type matchedRange struct {
	start int
	end   int
}

func finalizeSummaryText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return summaryHashtag
	}
	if strings.HasSuffix(text, summaryHashtag) {
		return text
	}
	return text + "\n\n" + summaryHashtag
}

func buildBoldNameFormatData(text string, messages []PreparedMessage) string {
	names := uniqueSenderNames(messages)
	if len(names) == 0 {
		return ""
	}

	sort.SliceStable(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	matches := make([]matchedRange, 0, len(names))
	items := make([]formatItem, 0, len(names))
	for _, name := range names {
		start := 0
		for {
			idx := strings.Index(text[start:], name)
			if idx < 0 {
				break
			}

			matchStart := start + idx
			matchEnd := matchStart + len(name)
			start = matchEnd

			if !isNameBoundary(text, matchStart, matchEnd) {
				continue
			}
			if overlapsExisting(matches, matchStart, matchEnd) {
				continue
			}

			matches = append(matches, matchedRange{start: matchStart, end: matchEnd})
			items = append(items, formatItem{
				Type:   "bold",
				Offset: utf16Units(text[:matchStart]),
				Length: utf16Units(name),
			})
		}
	}

	if len(items) == 0 {
		return ""
	}

	data, err := json.Marshal(formatData{Version: 1, Items: items})
	if err != nil {
		return ""
	}
	return string(data)
}

func uniqueSenderNames(messages []PreparedMessage) []string {
	seen := make(map[string]struct{}, len(messages))
	names := make([]string, 0, len(messages))
	for _, message := range messages {
		name := strings.TrimSpace(message.SenderName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func isNameBoundary(text string, start, end int) bool {
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if isNameRune(prev) {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if isNameRune(next) {
			return false
		}
	}
	return true
}

func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func overlapsExisting(existing []matchedRange, start, end int) bool {
	for _, rng := range existing {
		if start < rng.end && end > rng.start {
			return true
		}
	}
	return false
}

func utf16Units(text string) int {
	return len(utf16.Encode([]rune(text)))
}
