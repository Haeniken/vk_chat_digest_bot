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
	ChatID                 int64
	PeerID                 int64
	FirstMessageID         int64
	LastMessageID          int64
	FirstSentAt            time.Time
	LastSentAt             time.Time
	RawMessageCount        int
	MeaningfulMessageCount int
	SummaryText            string
	IssueNumber            int64
	LLMProvider            string
	TriggerSource          string
	PublishedAt            time.Time
}

type SummaryChatState struct {
	ChatID                     int64
	PeerID                     int64
	NextAttemptMeaningfulCount int
	LastRateLimitNoticeAt      *time.Time
}
