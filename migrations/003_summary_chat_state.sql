CREATE TABLE IF NOT EXISTS summary_chat_state (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    peer_id BIGINT NOT NULL UNIQUE,
    next_attempt_meaningful_count INTEGER NOT NULL,
    last_rate_limit_notice_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_summary_chat_state_peer
    ON summary_chat_state (peer_id);
