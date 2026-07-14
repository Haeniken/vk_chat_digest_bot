package storage

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	migrationfiles "bot-summary-vk/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

const maxStoredPublishedSummariesPerPeer = 10

func New(ctx context.Context, databaseURL string, maxConns, minConns int32, connectTimeout, queryTimeout time.Duration) (*Repository, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	poolConfig.MaxConns = maxConns
	poolConfig.MinConns = minConns
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute
	poolConfig.ConnConfig.ConnectTimeout = connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	repo := &Repository{pool: pool, queryTimeout: queryTimeout}
	if err := repo.ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := repo.runMigrations(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return repo, nil
}

func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *Repository) ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (r *Repository) runMigrations(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if _, err := r.pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())"); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	entries, err := migrationfiles.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versions = append(versions, entry.Name())
	}
	sort.Strings(versions)

	for _, version := range versions {
		applied, err := r.isMigrationApplied(ctx, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationfiles.Files.ReadFile(version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}

	return nil
}

func (r *Repository) isMigrationApplied(ctx context.Context, version string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return exists, nil
}

func (r *Repository) SaveMessage(ctx context.Context, message Message) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx, `
        INSERT INTO messages (
            source_message_id,
            conversation_message_id,
            chat_id,
            peer_id,
            sender_id,
            sender_name,
            text,
            reply_to_source_message_id,
            reply_to_conversation_message_id,
            reply_to_sender_id,
            reply_to_sender_name,
            reply_to_text,
            sent_at,
            received_at,
            is_outgoing,
            updated_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),$11,$12,$13,$14,$15,NOW())
        ON CONFLICT (peer_id, source_message_id) DO UPDATE SET
            conversation_message_id = EXCLUDED.conversation_message_id,
            sender_id = EXCLUDED.sender_id,
            sender_name = EXCLUDED.sender_name,
            text = EXCLUDED.text,
            reply_to_source_message_id = EXCLUDED.reply_to_source_message_id,
            reply_to_conversation_message_id = EXCLUDED.reply_to_conversation_message_id,
            reply_to_sender_id = EXCLUDED.reply_to_sender_id,
            reply_to_sender_name = EXCLUDED.reply_to_sender_name,
            reply_to_text = EXCLUDED.reply_to_text,
            sent_at = EXCLUDED.sent_at,
            is_outgoing = EXCLUDED.is_outgoing,
            updated_at = NOW()
    `, message.SourceMessageID, message.ConversationMessageID, message.ChatID, message.PeerID, message.SenderID, message.SenderName, message.Text, message.ReplyToSourceMessageID, message.ReplyToConversationMessageID, message.ReplyToSenderID, message.ReplyToSenderName, message.ReplyToText, message.SentAt.UTC(), message.ReceivedAt.UTC(), message.IsOutgoing)
	if err != nil {
		return fmt.Errorf("save message %d: %w", message.SourceMessageID, err)
	}
	return nil
}

func (r *Repository) ListMessagesByWindow(ctx context.Context, peerID int64, start, end time.Time) ([]Message, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
        SELECT id, source_message_id, conversation_message_id, chat_id, peer_id, sender_id, sender_name, text,
               COALESCE(reply_to_source_message_id, 0),
               COALESCE(reply_to_conversation_message_id, 0),
               COALESCE(reply_to_sender_id, 0),
               reply_to_sender_name,
               reply_to_text,
               sent_at, received_at, is_outgoing
        FROM messages
        WHERE peer_id = $1 AND sent_at >= $2 AND sent_at < $3
        ORDER BY sent_at ASC, source_message_id ASC
    `, peerID, start.UTC(), end.UTC())
	if err != nil {
		return nil, fmt.Errorf("list messages for window: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.SourceMessageID, &message.ConversationMessageID, &message.ChatID, &message.PeerID, &message.SenderID, &message.SenderName, &message.Text, &message.ReplyToSourceMessageID, &message.ReplyToConversationMessageID, &message.ReplyToSenderID, &message.ReplyToSenderName, &message.ReplyToText, &message.SentAt, &message.ReceivedAt, &message.IsOutgoing); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message rows: %w", err)
	}
	return messages, nil
}

func (r *Repository) ListMessagesAfterID(ctx context.Context, peerID, afterID int64, limit int) ([]Message, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
        SELECT id, source_message_id, conversation_message_id, chat_id, peer_id, sender_id, sender_name, text,
               COALESCE(reply_to_source_message_id, 0),
               COALESCE(reply_to_conversation_message_id, 0),
               COALESCE(reply_to_sender_id, 0),
               reply_to_sender_name,
               reply_to_text,
               sent_at, received_at, is_outgoing
        FROM messages
        WHERE peer_id = $1 AND id > $2
        ORDER BY id ASC
        LIMIT $3
    `, peerID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list messages after id: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0, limit)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.SourceMessageID, &message.ConversationMessageID, &message.ChatID, &message.PeerID, &message.SenderID, &message.SenderName, &message.Text, &message.ReplyToSourceMessageID, &message.ReplyToConversationMessageID, &message.ReplyToSenderID, &message.ReplyToSenderName, &message.ReplyToText, &message.SentAt, &message.ReceivedAt, &message.IsOutgoing); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate message rows: %w", err)
	}
	return messages, nil
}

func (r *Repository) LastProcessedMessageID(ctx context.Context, peerID int64) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var lastMessageID int64
	var currentMaxID int64
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT MAX(last_message_id) FROM processed_summary_batches WHERE peer_id = $1), 0),
			COALESCE((SELECT MAX(id) FROM messages WHERE peer_id = $1), 0)
	`, peerID).Scan(&lastMessageID, &currentMaxID); err != nil {
		return 0, fmt.Errorf("select last processed message id: %w", err)
	}
	if currentMaxID > 0 && lastMessageID > currentMaxID {
		return 0, nil
	}
	return lastMessageID, nil
}

func (r *Repository) LastPublishedSummaries(ctx context.Context, peerID int64, limit int) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if limit <= 0 {
		limit = 1
	}

	rows, err := r.pool.Query(ctx, `
        WITH recent AS (
            SELECT summary_text, published_at, id
            FROM processed_summary_batches
            WHERE peer_id = $1
            ORDER BY published_at DESC, id DESC
            LIMIT $2
        )
        SELECT summary_text
        FROM recent
        ORDER BY published_at ASC, id ASC
    `, peerID, limit)
	if err != nil {
		return nil, fmt.Errorf("select last published summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]string, 0, limit)
	for rows.Next() {
		var summaryText string
		if err := rows.Scan(&summaryText); err != nil {
			return nil, fmt.Errorf("scan last published summary: %w", err)
		}
		summaries = append(summaries, summaryText)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate last published summaries: %w", err)
	}
	return summaries, nil
}

func (r *Repository) GetSummaryChatState(ctx context.Context, chatID, peerID int64, defaultNextAttempt int) (SummaryChatState, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if _, err := r.pool.Exec(ctx, `
        INSERT INTO summary_chat_state (chat_id, peer_id, next_attempt_meaningful_count, updated_at)
        VALUES ($1, $2, $3, NOW())
        ON CONFLICT (peer_id) DO NOTHING
    `, chatID, peerID, defaultNextAttempt); err != nil {
		return SummaryChatState{}, fmt.Errorf("ensure summary chat state: %w", err)
	}

	var state SummaryChatState
	if err := r.pool.QueryRow(ctx, `
        SELECT chat_id, peer_id, next_attempt_meaningful_count, last_rate_limit_notice_at
        FROM summary_chat_state
        WHERE peer_id = $1
    `, peerID).Scan(&state.ChatID, &state.PeerID, &state.NextAttemptMeaningfulCount, &state.LastRateLimitNoticeAt); err != nil {
		return SummaryChatState{}, fmt.Errorf("select summary chat state: %w", err)
	}
	return state, nil
}

func (r *Repository) ResetSummaryChatState(ctx context.Context, chatID, peerID int64, defaultNextAttempt int) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx, `
        INSERT INTO summary_chat_state (chat_id, peer_id, next_attempt_meaningful_count, last_rate_limit_notice_at, updated_at)
        VALUES ($1, $2, $3, NULL, NOW())
        ON CONFLICT (peer_id) DO UPDATE SET
            chat_id = EXCLUDED.chat_id,
            next_attempt_meaningful_count = EXCLUDED.next_attempt_meaningful_count,
            last_rate_limit_notice_at = NULL,
            updated_at = NOW()
    `, chatID, peerID, defaultNextAttempt)
	if err != nil {
		return fmt.Errorf("reset summary chat state: %w", err)
	}
	return nil
}

func (r *Repository) AdvanceSummaryChatRateLimit(ctx context.Context, chatID, peerID int64, nextAttemptMeaningfulCount int, noticedAt time.Time) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx, `
        INSERT INTO summary_chat_state (chat_id, peer_id, next_attempt_meaningful_count, last_rate_limit_notice_at, updated_at)
        VALUES ($1, $2, $3, $4, NOW())
        ON CONFLICT (peer_id) DO UPDATE SET
            chat_id = EXCLUDED.chat_id,
            next_attempt_meaningful_count = GREATEST(summary_chat_state.next_attempt_meaningful_count, EXCLUDED.next_attempt_meaningful_count),
            last_rate_limit_notice_at = EXCLUDED.last_rate_limit_notice_at,
            updated_at = NOW()
    `, chatID, peerID, nextAttemptMeaningfulCount, noticedAt.UTC())
	if err != nil {
		return fmt.Errorf("advance summary chat rate limit state: %w", err)
	}
	return nil
}

func (r *Repository) ReserveSummaryIssueNumber(ctx context.Context, peerID int64) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var issueNumber int64
	if err := r.pool.QueryRow(ctx, `
        INSERT INTO summary_issue_counters (peer_id, next_issue_number, updated_at)
        VALUES ($1, 2, NOW())
        ON CONFLICT (peer_id) DO UPDATE SET
            next_issue_number = summary_issue_counters.next_issue_number + 1,
            updated_at = NOW()
        RETURNING next_issue_number - 1
    `, peerID).Scan(&issueNumber); err != nil {
		return 0, fmt.Errorf("reserve summary issue number: %w", err)
	}
	return issueNumber, nil
}

func (r *Repository) IsBatchProcessed(ctx context.Context, peerID, firstMessageID, lastMessageID int64) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var exists bool
	if err := r.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM processed_summary_batches
            WHERE peer_id = $1 AND first_message_id = $2 AND last_message_id = $3
        )
    `, peerID, firstMessageID, lastMessageID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check processed batch: %w", err)
	}
	return exists, nil
}

func (r *Repository) MarkBatchPublished(ctx context.Context, batch PublishedSummaryBatch) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin finalize batch: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
        INSERT INTO processed_summary_batches (
            chat_id,
            peer_id,
            first_message_id,
            last_message_id,
            first_sent_at,
            last_sent_at,
            raw_message_count,
            meaningful_message_count,
            summary_text,
            issue_number,
            llm_provider,
            llm_model,
            llm_prompt_tokens,
            llm_cached_prompt_tokens,
            llm_completion_tokens,
            llm_latency_ms,
            image_prompt_llm_provider,
            image_prompt_llm_model,
            image_prompt_llm_prompt_tokens,
            image_prompt_llm_cached_prompt_tokens,
            image_prompt_llm_completion_tokens,
            image_prompt_llm_latency_ms,
            image_provider,
            image_model,
            image_input_tokens,
            image_input_text_tokens,
            image_input_image_tokens,
            image_output_tokens,
            image_latency_ms,
            image_published,
            trigger_source,
            published_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32)
        ON CONFLICT (peer_id, first_message_id, last_message_id) DO NOTHING
    `, batch.ChatID, batch.PeerID, batch.FirstMessageID, batch.LastMessageID, batch.FirstSentAt.UTC(), batch.LastSentAt.UTC(), batch.RawMessageCount, batch.MeaningfulMessageCount, batch.SummaryText, batch.IssueNumber, batch.LLMProvider, batch.LLMModel, batch.LLMPromptTokens, batch.LLMCachedPromptTokens, batch.LLMCompletionTokens, batch.LLMLatencyMs, batch.ImagePromptLLMProvider, batch.ImagePromptLLMModel, batch.ImagePromptLLMPromptTokens, batch.ImagePromptLLMCachedPromptTokens, batch.ImagePromptLLMCompletionTokens, batch.ImagePromptLLMLatencyMs, batch.ImageProvider, batch.ImageModel, batch.ImageInputTokens, batch.ImageInputTextTokens, batch.ImageInputImageTokens, batch.ImageOutputTokens, batch.ImageLatencyMs, batch.ImagePublished, batch.TriggerSource, batch.PublishedAt.UTC()); err != nil {
		return fmt.Errorf("insert processed batch: %w", err)
	}

	if _, err := tx.Exec(ctx, `
        DELETE FROM messages
        WHERE peer_id = $1 AND id <= $2
    `, batch.PeerID, batch.LastMessageID); err != nil {
		return fmt.Errorf("delete processed messages: %w", err)
	}

	if _, err := tx.Exec(ctx, `
        DELETE FROM processed_summary_batches
        WHERE peer_id = $1
          AND id IN (
              SELECT id
              FROM processed_summary_batches
              WHERE peer_id = $1
              ORDER BY published_at DESC, id DESC
              OFFSET $2
          )
    `, batch.PeerID, maxStoredPublishedSummariesPerPeer); err != nil {
		return fmt.Errorf("prune old published summaries: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit finalize batch: %w", err)
	}
	return nil
}

func (r *Repository) AcquireWindowLock(ctx context.Context, peerID int64, start, end time.Time) (func(), bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire advisory lock connection: %w", err)
	}

	lockKey := advisoryLockKey(peerID, start, end)
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&locked); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, false, nil
	}

	unlock := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", lockKey)
		conn.Release()
	}

	return unlock, true, nil
}

func (r *Repository) AcquireBatchLock(ctx context.Context, peerID, firstMessageID, lastMessageID int64) (func(), bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire advisory lock connection: %w", err)
	}

	lockKey := advisoryLockKeyForBatch(peerID, firstMessageID, lastMessageID)
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&locked); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, false, nil
	}

	unlock := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", lockKey)
		conn.Release()
	}

	return unlock, true, nil
}

func (r *Repository) withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, r.queryTimeout)
}

func advisoryLockKey(peerID int64, start, end time.Time) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d:%d", peerID, start.UTC().Unix(), end.UTC().Unix())))
	return int64(h.Sum64())
}

func advisoryLockKeyForBatch(peerID, firstMessageID, lastMessageID int64) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d:%d", peerID, firstMessageID, lastMessageID)))
	return int64(h.Sum64())
}
