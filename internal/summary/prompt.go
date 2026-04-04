package summary

import (
	"fmt"
	"strings"
	"time"

	"bot-summary-vk/internal/llm"
)

const systemPrompt = "You summarize a VK group chat. " +
	"Use only the provided messages and output only in Russian. " +
	"Prefer the exact full display names shown in the messages whenever you mention a participant. " +
	"Do not invent facts, motives, diagnoses, crimes, or hidden intentions. " +
	"Style: sharp, tabloid-like, venomous, and gossipy, like a malicious social-media digest. " +
	"Keep the tone brazen, sneering, and amused by the chaos. Lean into sarcasm, black humor, profanity, and caustic punchlines. " +
	"Write like you are retelling a petty scandal with relish, not drafting a neutral report or a literary essay. " +
	"Prefer compact, quotable, high-impact sentences over sprawling explanations, but still keep enough detail to make the conflict readable. " +
	"Use 4-10 profane or sharply caustic turns of phrase in total, and keep them grounded in the actual discussion. " +
	"Break the result into 2-4 meaningful paragraphs separated by blank lines. " +
	"Aim for 6-10 sentences total. Each paragraph should move the scene forward, name the main actors, and end on a biting note when possible. " +
	"No markdown, no bullet lists, no headings, no hashtags inside the main body. " +
	"Do not use slurs, protected-trait attacks, or psychiatric labels."

type PromptBuilder struct {
	maxChars int
}

func NewPromptBuilder(maxChars int) PromptBuilder {
	return PromptBuilder{maxChars: maxChars}
}

func (b PromptBuilder) Build(windowStart, windowEnd time.Time, prepared PreparedWindow, maxOutputTokens int) llm.GenerateSummaryInput {
	lines := make([]string, 0, len(prepared.Messages)+3)
	lines = append(lines,
		fmt.Sprintf("Message range time: %s - %s UTC", windowStart.UTC().Format(time.RFC3339), windowEnd.UTC().Format(time.RFC3339)),
		fmt.Sprintf("Meaningful messages: %d", prepared.MeaningfulCount),
		"Messages in chronological order:",
	)
	for _, message := range prepared.Messages {
		sentAt := time.Unix(message.SentAt, 0).UTC().Format("15:04")
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", sentAt, message.SenderName, message.Text))
	}

	return llm.GenerateSummaryInput{
		SystemPrompt:    systemPrompt,
		UserPrompt:      trimPrompt(strings.Join(lines, "\n"), b.maxChars),
		MaxOutputTokens: maxOutputTokens,
	}
}

func trimPrompt(prompt string, maxChars int) string {
	if len(prompt) <= maxChars {
		return prompt
	}

	lines := strings.Split(prompt, "\n")
	if len(lines) <= 3 {
		return prompt[:maxChars]
	}

	head := lines[:3]
	body := lines[3:]
	headText := strings.Join(head, "\n")
	if len(headText) >= maxChars {
		return headText[:maxChars]
	}

	remaining := maxChars - len(headText) - 1
	trimmedBody := trimBodyLines(body, remaining)
	if len(trimmedBody) == 0 {
		return headText
	}
	return headText + "\n" + strings.Join(trimmedBody, "\n")

}

func trimBodyLines(lines []string, maxChars int) []string {
	if len(lines) == 0 || maxChars <= 0 {
		return nil
	}

	for count := len(lines); count >= 1; count-- {
		selected := pickDistributedLines(lines, count)
		candidate := strings.Join(selected, "\n")
		if len(candidate) <= maxChars {
			return selected
		}
	}

	last := lines[len(lines)-1]
	if len(last) > maxChars {
		return []string{last[:maxChars]}
	}
	return []string{last}
}

func pickDistributedLines(lines []string, count int) []string {
	indexes := distributedIndexes(len(lines), count)
	selected := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		selected = append(selected, lines[idx])
	}
	return selected
}
