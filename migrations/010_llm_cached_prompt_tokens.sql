ALTER TABLE processed_summary_batches
    ADD COLUMN IF NOT EXISTS llm_cached_prompt_tokens INTEGER NOT NULL DEFAULT 0;
