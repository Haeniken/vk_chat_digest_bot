package llm

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type StubClient struct {
	model string
}

func NewStubClient(model string) *StubClient {
	return &StubClient{model: model}
}

func (c *StubClient) Provider() string {
	return "stub"
}

func (c *StubClient) GenerateSummary(_ context.Context, input GenerateSummaryInput) (GenerateSummaryOutput, error) {
	lines := strings.Split(input.UserPrompt, "\n")
	keywords := collectKeywords(lines)
	messageCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "[") && strings.Contains(line, ": ") {
			messageCount++
		}
	}

	headline := "Za poslednie 6 chasov chat snova kollektivno reshhal, chto dramatichnyj shum - eto tozhe plan."
	if len(keywords) > 0 {
		headline = fmt.Sprintf("Za poslednie 6 chasov uchastniki s pochatitelnoj serioznostyu krutilis vokrug tem: %s.", strings.Join(keywords, ", "))
	}
	body := fmt.Sprintf("Itog: %d osmyslennyh soobshchenij, nemnogo suety i odin syuzhet, kotoryj vse zhe udalos soberat bez arheologicheskoj ekspedicii.", messageCount)
	return GenerateSummaryOutput{Text: headline + " " + body}, nil
}

func collectKeywords(lines []string) []string {
	re := regexp.MustCompile(`[\p{L}\p{N}]{5,}`)
	counts := map[string]int{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		for _, token := range re.FindAllString(strings.ToLower(parts[1]), -1) {
			counts[token]++
		}
	}

	type pair struct {
		word  string
		count int
	}
	items := make([]pair, 0, len(counts))
	for word, count := range counts {
		items = append(items, pair{word: word, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].word < items[j].word
		}
		return items[i].count > items[j].count
	})

	result := make([]string, 0, 3)
	for _, item := range items {
		result = append(result, item.word)
		if len(result) == 3 {
			break
		}
	}
	return result
}
