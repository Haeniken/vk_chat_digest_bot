CREATE TABLE IF NOT EXISTS processed_summary_batches (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    peer_id BIGINT NOT NULL,
    first_message_id BIGINT NOT NULL,
    last_message_id BIGINT NOT NULL,
    first_sent_at TIMESTAMPTZ NOT NULL,
    last_sent_at TIMESTAMPTZ NOT NULL,
    raw_message_count INTEGER NOT NULL,
    meaningful_message_count INTEGER NOT NULL,
    summary_text TEXT NOT NULL,
    llm_provider TEXT NOT NULL,
    trigger_source TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (peer_id, first_message_id, last_message_id)
);

CREATE INDEX IF NOT EXISTS idx_processed_summary_batches_peer_last_message
    ON processed_summary_batches (peer_id, last_message_id);

CREATE INDEX IF NOT EXISTS idx_processed_summary_batches_peer_range
    ON processed_summary_batches (peer_id, first_message_id, last_message_id);
