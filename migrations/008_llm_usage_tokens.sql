ALTER TABLE processed_summary_batches
    ADD COLUMN IF NOT EXISTS llm_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS llm_prompt_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS llm_completion_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS llm_latency_ms BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_processed_summary_batches_published_at
    ON processed_summary_batches (published_at);
