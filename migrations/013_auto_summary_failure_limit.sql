ALTER TABLE summary_chat_state
    ADD COLUMN IF NOT EXISTS auto_failure_count INTEGER NOT NULL DEFAULT 0;
