package summary

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

type progressPublisher interface {
	PublishProgressMessage(ctx context.Context, peerID int64, text string) (int64, error)
	EditProgressMessage(ctx context.Context, peerID, conversationMessageID int64, text string) error
	DeleteProgressMessage(ctx context.Context, peerID, conversationMessageID int64) error
}

type summaryProgress struct {
	publisher             progressPublisher
	logger                *slog.Logger
	peerID                int64
	conversationMessageID int64
	cancel                context.CancelFunc
	done                  chan struct{}
	once                  sync.Once
}

func (s *Service) startSummaryProgress(ctx context.Context, peerID int64, afterConversationMessageID int64) *summaryProgress {
	publisher, ok := s.publisher.(progressPublisher)
	if !ok {
		return nil
	}

	phrase, phraseIndex := randomSummaryProgressPhrase(-1)
	text := formatSummaryProgressText(phrase)
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	conversationMessageID, err := publisher.PublishProgressMessage(sendCtx, peerID, text)
	cancel()
	if err != nil {
		s.logger.Warn("failed to publish summary progress message",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if conversationMessageID <= 0 {
		discoveryPhrase, discoveryPhraseIndex := randomSummaryProgressPhrase(phraseIndex)
		discoveryText := formatSummaryProgressText(discoveryPhrase)
		if discoveredID := s.discoverSummaryProgressMessage(ctx, publisher, peerID, afterConversationMessageID, discoveryText); discoveredID > 0 {
			conversationMessageID = discoveredID
			phraseIndex = discoveryPhraseIndex
		}
	}
	if conversationMessageID <= 0 {
		return nil
	}

	progressCtx, progressCancel := context.WithCancel(ctx)
	progress := &summaryProgress{
		publisher:             publisher,
		logger:                s.logger,
		peerID:                peerID,
		conversationMessageID: conversationMessageID,
		cancel:                progressCancel,
		done:                  make(chan struct{}),
	}
	go progress.run(progressCtx, phraseIndex)
	return progress
}

func (s *Service) discoverSummaryProgressMessage(ctx context.Context, publisher progressPublisher, peerID int64, afterConversationMessageID int64, text string) int64 {
	if afterConversationMessageID <= 0 {
		s.logger.Warn("summary progress message has empty conversation id and no lookup anchor",
			slog.Int64("peer_id", peerID),
		)
		return 0
	}

	const lookupWindow int64 = 25
	for conversationMessageID := afterConversationMessageID + 1; conversationMessageID <= afterConversationMessageID+lookupWindow; conversationMessageID++ {
		editCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := publisher.EditProgressMessage(editCtx, peerID, conversationMessageID, text)
		cancel()
		if err == nil {
			return conversationMessageID
		}
	}

	s.logger.Warn("failed to discover summary progress conversation id",
		slog.Int64("peer_id", peerID),
		slog.Int64("after_conversation_message_id", afterConversationMessageID),
		slog.Int64("lookup_window", lookupWindow),
	)
	return 0
}

func (p *summaryProgress) Stop() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.cancel()
		<-p.done

		deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.publisher.DeleteProgressMessage(deleteCtx, p.peerID, p.conversationMessageID); err != nil {
			p.logger.Warn("failed to delete summary progress message",
				slog.Int64("peer_id", p.peerID),
				slog.Int64("conversation_message_id", p.conversationMessageID),
				slog.String("error", err.Error()),
			)
		}
	})
}

func (p *summaryProgress) run(ctx context.Context, lastPhraseIndex int) {
	defer close(p.done)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	warnedEditFailure := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			phrase, phraseIndex := randomSummaryProgressPhrase(lastPhraseIndex)
			lastPhraseIndex = phraseIndex

			editCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := p.publisher.EditProgressMessage(editCtx, p.peerID, p.conversationMessageID, formatSummaryProgressText(phrase))
			cancel()
			if err != nil && !warnedEditFailure {
				warnedEditFailure = true
				p.logger.Warn("failed to edit summary progress message",
					slog.Int64("peer_id", p.peerID),
					slog.Int64("conversation_message_id", p.conversationMessageID),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

func randomSummaryProgressPhrase(except int) (string, int) {
	if len(summaryProgressPhrases) == 0 {
		return "В редакции готовится новый выпуск...", -1
	}
	idx := rand.Intn(len(summaryProgressPhrases))
	if len(summaryProgressPhrases) > 1 && idx == except {
		idx = (idx + 1 + rand.Intn(len(summaryProgressPhrases)-1)) % len(summaryProgressPhrases)
	}
	return summaryProgressPhrases[idx], idx
}

func formatSummaryProgressText(phrase string) string {
	return fmt.Sprintf("🗞 В редакции готовится новый выпуск.\n%s", phrase)
}
