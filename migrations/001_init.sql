CREATE TABLE IF NOT EXISTS messages (
    id BIGSERIAL PRIMARY KEY,
    source_message_id BIGINT NOT NULL,
    conversation_message_id BIGINT,
    chat_id BIGINT NOT NULL,
    peer_id BIGINT NOT NULL,
    sender_id BIGINT NOT NULL,
    text TEXT NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_outgoing BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (peer_id, source_message_id)
);

CREATE INDEX IF NOT EXISTS idx_messages_peer_sent_at ON messages (peer_id, sent_at);
CREATE INDEX IF NOT EXISTS idx_messages_chat_sent_at ON messages (chat_id, sent_at);

CREATE TABLE IF NOT EXISTS processed_summary_windows (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    peer_id BIGINT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    message_count INTEGER NOT NULL,
    summary_text TEXT NOT NULL,
    llm_provider TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (peer_id, window_start, window_end)
);

CREATE INDEX IF NOT EXISTS idx_processed_summary_windows_peer_window
    ON processed_summary_windows (peer_id, window_start, window_end);
