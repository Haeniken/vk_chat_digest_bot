package storage

import "time"

type Message struct {
	ID                           int64
	SourceMessageID              int64
	ConversationMessageID        int64
	ChatID                       int64
	PeerID                       int64
	SenderID                     int64
	SenderName                   string
	Text                         string
	ReplyToSourceMessageID       int64
	ReplyToConversationMessageID int64
	ReplyToSenderID              int64
	ReplyToSenderName            string
	ReplyToText                  string
	SentAt                       time.Time
	ReceivedAt                   time.Time
	IsOutgoing                   bool
}

type PublishedSummaryBatch struct {
	ChatID                           int64
	PeerID                           int64
	FirstMessageID                   int64
	LastMessageID                    int64
	FirstSentAt                      time.Time
	LastSentAt                       time.Time
	RawMessageCount                  int
	MeaningfulMessageCount           int
	SummaryText                      string
	IssueNumber                      int64
	LLMProvider                      string
	LLMModel                         string
	LLMPromptTokens                  int
	LLMCachedPromptTokens            int
	LLMCompletionTokens              int
	LLMLatencyMs                     int64
	ImagePromptLLMProvider           string
	ImagePromptLLMModel              string
	ImagePromptLLMPromptTokens       int
	ImagePromptLLMCachedPromptTokens int
	ImagePromptLLMCompletionTokens   int
	ImagePromptLLMLatencyMs          int64
	ImageProvider                    string
	ImageModel                       string
	ImageInputTokens                 int
	ImageInputTextTokens             int
	ImageInputImageTokens            int
	ImageOutputTokens                int
	ImageLatencyMs                   int64
	ImagePublished                   bool
	TriggerSource                    string
	PublishedAt                      time.Time
}

type SummaryChatState struct {
	ChatID                     int64
	PeerID                     int64
	NextAttemptMeaningfulCount int
	LastRateLimitNoticeAt      *time.Time
}

type LLMUsageTotals struct {
	SummaryCount       int
	ChatCount          int
	PromptTokens       int64
	CachedPromptTokens int64
	CompletionTokens   int64
	AvgLatencyMs       int64
}

type DailyLLMUsage struct {
	Day                string
	SummaryCount       int
	ChatCount          int
	PromptTokens       int64
	CachedPromptTokens int64
	CompletionTokens   int64
	AvgLatencyMs       int64
}

type ImageUsageTotals struct {
	ImageCount                  int
	ChatCount                   int
	PromptLLMPromptTokens       int64
	PromptLLMCachedPromptTokens int64
	PromptLLMCompletionTokens   int64
	ImageInputTokens            int64
	ImageInputTextTokens        int64
	ImageInputImageTokens       int64
	ImageOutputTokens           int64
	AvgPromptLLMLatencyMs       int64
	AvgImageLatencyMs           int64
}

type DailyImageUsage struct {
	Day                         string
	ImageCount                  int
	ChatCount                   int
	PromptLLMPromptTokens       int64
	PromptLLMCachedPromptTokens int64
	PromptLLMCompletionTokens   int64
	ImageInputTokens            int64
	ImageInputTextTokens        int64
	ImageInputImageTokens       int64
	ImageOutputTokens           int64
	AvgPromptLLMLatencyMs       int64
	AvgImageLatencyMs           int64
}
