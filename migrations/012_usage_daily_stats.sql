CREATE TABLE IF NOT EXISTS usage_daily_stats (
    day DATE NOT NULL,
    peer_id BIGINT NOT NULL,
    summary_count INTEGER NOT NULL DEFAULT 0,
    llm_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    llm_cached_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    llm_completion_tokens BIGINT NOT NULL DEFAULT 0,
    llm_latency_total_ms BIGINT NOT NULL DEFAULT 0,
    llm_latency_count INTEGER NOT NULL DEFAULT 0,
    image_count INTEGER NOT NULL DEFAULT 0,
    image_prompt_llm_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    image_prompt_llm_cached_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    image_prompt_llm_completion_tokens BIGINT NOT NULL DEFAULT 0,
    image_prompt_llm_latency_total_ms BIGINT NOT NULL DEFAULT 0,
    image_prompt_llm_latency_count INTEGER NOT NULL DEFAULT 0,
    image_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_text_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_image_tokens BIGINT NOT NULL DEFAULT 0,
    image_output_tokens BIGINT NOT NULL DEFAULT 0,
    image_latency_total_ms BIGINT NOT NULL DEFAULT 0,
    image_latency_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (day, peer_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_daily_stats_day
    ON usage_daily_stats (day);

CREATE INDEX IF NOT EXISTS idx_messages_received_at
    ON messages (received_at);

INSERT INTO usage_daily_stats (
    day,
    peer_id,
    summary_count,
    llm_prompt_tokens,
    llm_cached_prompt_tokens,
    llm_completion_tokens,
    llm_latency_total_ms,
    llm_latency_count,
    image_count,
    image_prompt_llm_prompt_tokens,
    image_prompt_llm_cached_prompt_tokens,
    image_prompt_llm_completion_tokens,
    image_prompt_llm_latency_total_ms,
    image_prompt_llm_latency_count,
    image_input_tokens,
    image_input_text_tokens,
    image_input_image_tokens,
    image_output_tokens,
    image_latency_total_ms,
    image_latency_count
)
SELECT
    timezone('Europe/Moscow', published_at)::date AS day,
    peer_id,
    COUNT(*)::integer AS summary_count,
    COALESCE(SUM(llm_prompt_tokens), 0)::bigint AS llm_prompt_tokens,
    COALESCE(SUM(llm_cached_prompt_tokens), 0)::bigint AS llm_cached_prompt_tokens,
    COALESCE(SUM(llm_completion_tokens), 0)::bigint AS llm_completion_tokens,
    COALESCE(SUM(llm_latency_ms) FILTER (WHERE llm_latency_ms > 0), 0)::bigint AS llm_latency_total_ms,
    (COUNT(*) FILTER (WHERE llm_latency_ms > 0))::integer AS llm_latency_count,
    (COUNT(*) FILTER (WHERE image_published))::integer AS image_count,
    COALESCE(SUM(image_prompt_llm_prompt_tokens), 0)::bigint AS image_prompt_llm_prompt_tokens,
    COALESCE(SUM(image_prompt_llm_cached_prompt_tokens), 0)::bigint AS image_prompt_llm_cached_prompt_tokens,
    COALESCE(SUM(image_prompt_llm_completion_tokens), 0)::bigint AS image_prompt_llm_completion_tokens,
    COALESCE(SUM(image_prompt_llm_latency_ms) FILTER (WHERE image_prompt_llm_latency_ms > 0), 0)::bigint AS image_prompt_llm_latency_total_ms,
    (COUNT(*) FILTER (WHERE image_prompt_llm_latency_ms > 0))::integer AS image_prompt_llm_latency_count,
    COALESCE(SUM(image_input_tokens), 0)::bigint AS image_input_tokens,
    COALESCE(SUM(image_input_text_tokens), 0)::bigint AS image_input_text_tokens,
    COALESCE(SUM(image_input_image_tokens), 0)::bigint AS image_input_image_tokens,
    COALESCE(SUM(image_output_tokens), 0)::bigint AS image_output_tokens,
    COALESCE(SUM(image_latency_ms) FILTER (WHERE image_latency_ms > 0), 0)::bigint AS image_latency_total_ms,
    (COUNT(*) FILTER (WHERE image_latency_ms > 0))::integer AS image_latency_count
FROM processed_summary_batches
GROUP BY 1, 2
ON CONFLICT (day, peer_id) DO NOTHING;
