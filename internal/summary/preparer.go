package summary

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"bot-summary-vk/internal/storage"
)

var urlOnlyPattern = regexp.MustCompile(`^(https?://\S+|www\.\S+)$`)
var slashCommandPattern = regexp.MustCompile(`^/[\pL\pN_:-]+$`)

type PreparedMessage struct {
	SenderID   int64
	SenderName string
	Text       string
	SentAt     int64
}

type PreparedWindow struct {
	Messages        []PreparedMessage
	RawCount        int
	MeaningfulCount int
	DroppedCount    int
	TotalChars      int
}

type PrepareConfig struct {
	MinMessageLength   int
	MaxContextChars    int
	MaxContextMessages int
}

func PrepareMessages(messages []storage.Message, cfg PrepareConfig) PreparedWindow {
	filtered := make([]PreparedMessage, 0, len(messages))
	dropped := 0

	for _, message := range messages {
		if message.IsOutgoing {
			dropped++
			continue
		}
		text, ok := MeaningfulText(message.Text, cfg.MinMessageLength)
		if !ok {
			dropped++
			continue
		}
		filtered = append(filtered, PreparedMessage{
			SenderID:   message.SenderID,
			SenderName: normalizeSenderName(message.SenderName, message.SenderID),
			Text:       text,
			SentAt:     message.SentAt.UTC().Unix(),
		})
	}

	meaningfulCount := len(filtered)
	trimmed, totalChars := trimToBudget(filtered, cfg.MaxContextMessages, cfg.MaxContextChars)
	dropped += meaningfulCount - len(trimmed)

	return PreparedWindow{
		Messages:        trimmed,
		RawCount:        len(messages),
		MeaningfulCount: meaningfulCount,
		DroppedCount:    dropped,
		TotalChars:      totalChars,
	}
}

func trimToBudget(messages []PreparedMessage, maxMessages, maxChars int) ([]PreparedMessage, int) {
	if len(messages) == 0 {
		return nil, 0
	}

	countLimit := min(len(messages), maxMessages)
	for count := countLimit; count >= 1; count-- {
		selected, totalChars := pickDistributedMessages(messages, count, maxChars)
		if totalChars <= maxChars {
			return selected, totalChars
		}
	}

	msg := messages[len(messages)-1]
	msg.Text = truncateText(msg.Text, maxChars)
	return []PreparedMessage{msg}, textLen(msg.Text)
}

func pickDistributedMessages(messages []PreparedMessage, count, maxChars int) ([]PreparedMessage, int) {
	indexes := distributedIndexes(len(messages), count)
	selected := make([]PreparedMessage, 0, len(indexes))
	totalChars := 0

	for _, idx := range indexes {
		msg := messages[idx]
		selected = append(selected, msg)
		totalChars += textLen(msg.Text)
	}

	if len(selected) == 1 && totalChars > maxChars {
		selected[0].Text = truncateText(selected[0].Text, maxChars)
		totalChars = textLen(selected[0].Text)
	}

	return selected, totalChars
}

func distributedIndexes(total, count int) []int {
	if total <= 0 || count <= 0 {
		return nil
	}
	if count >= total {
		indexes := make([]int, total)
		for i := range total {
			indexes[i] = i
		}
		return indexes
	}
	if count == 1 {
		return []int{total - 1}
	}

	indexes := make([]int, 0, count)
	last := -1
	for i := 0; i < count; i++ {
		idx := i * (total - 1) / (count - 1)
		if idx == last {
			continue
		}
		indexes = append(indexes, idx)
		last = idx
	}

	for len(indexes) < count {
		candidate := indexes[len(indexes)-1] + 1
		if candidate >= total {
			break
		}
		indexes = append(indexes, candidate)
	}

	return indexes
}

func MeaningfulText(text string, minLength int) (string, bool) {
	text = normalize(text)
	if isGarbage(text, minLength) {
		return "", false
	}
	return text, true
}

func isGarbage(text string, minLength int) bool {
	if text == "" {
		return true
	}
	if utf8.RuneCountInString(text) < minLength {
		return true
	}
	if urlOnlyPattern.MatchString(text) {
		return true
	}
	if slashCommandPattern.MatchString(text) {
		return true
	}

	hasLetterOrDigit := false
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetterOrDigit = true
			break
		}
	}
	return !hasLetterOrDigit
}

func normalize(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func normalizeSenderName(name string, senderID int64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Sprintf("user_%d", senderID)
	}
	return name
}

func truncateText(text string, maxChars int) string {
	if textLen(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return firstRunes(text, maxChars)
	}
	return firstRunes(text, maxChars-3) + "..."
}

func firstRunes(text string, count int) string {
	if count <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= count {
		return text
	}
	return string(runes[:count])
}

func textLen(text string) int {
	return utf8.RuneCountInString(text)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
