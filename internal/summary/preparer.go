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
	SenderID                     int64
	SenderName                   string
	Text                         string
	ReplyToConversationMessageID int64
	ReplyToSenderName            string
	ReplyToText                  string
	SentAt                       int64
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
			SenderID:                     message.SenderID,
			SenderName:                   normalizeSenderName(message.SenderName, message.SenderID),
			Text:                         text,
			ReplyToConversationMessageID: message.ReplyToConversationMessageID,
			ReplyToSenderName:            normalizeSenderName(message.ReplyToSenderName, message.ReplyToSenderID),
			ReplyToText:                  replyPreview(message.ReplyToText, 160),
			SentAt:                       message.SentAt.UTC().Unix(),
		})
	}

	meaningfulCount := len(filtered)
	totalChars := 0
	for _, message := range filtered {
		totalChars += textLen(message.Text)
	}

	return PreparedWindow{
		Messages:        filtered,
		RawCount:        len(messages),
		MeaningfulCount: meaningfulCount,
		DroppedCount:    dropped,
		TotalChars:      totalChars,
	}
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
		if senderID <= 0 {
			return ""
		}
		return fmt.Sprintf("user_%d", senderID)
	}
	return name
}

func replyPreview(text string, maxRunes int) string {
	text = normalize(text)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func textLen(text string) int {
	return utf8.RuneCountInString(text)
}
