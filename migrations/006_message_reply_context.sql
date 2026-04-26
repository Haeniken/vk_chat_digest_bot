ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS reply_to_source_message_id BIGINT,
    ADD COLUMN IF NOT EXISTS reply_to_conversation_message_id BIGINT,
    ADD COLUMN IF NOT EXISTS reply_to_sender_id BIGINT,
    ADD COLUMN IF NOT EXISTS reply_to_sender_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reply_to_text TEXT NOT NULL DEFAULT '';

