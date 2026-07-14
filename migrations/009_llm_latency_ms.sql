ALTER TABLE processed_summary_batches
    ADD COLUMN IF NOT EXISTS llm_latency_ms BIGINT NOT NULL DEFAULT 0;
