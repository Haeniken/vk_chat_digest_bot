package storage

import (
	"context"
	"fmt"
)

func (r *Repository) LLMUsageDays(ctx context.Context, days int, timezone string) (LLMUsageTotals, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if days <= 0 {
		days = 30
	}
	if timezone == "" {
		timezone = "UTC"
	}

	var totals LLMUsageTotals
	if err := r.pool.QueryRow(ctx, `
        SELECT
            COUNT(*)::int,
            COUNT(DISTINCT peer_id)::int,
            COALESCE(SUM(llm_prompt_tokens), 0)::bigint,
            COALESCE(SUM(llm_cached_prompt_tokens), 0)::bigint,
            COALESCE(SUM(llm_completion_tokens), 0)::bigint,
            COALESCE(ROUND(AVG(NULLIF(llm_latency_ms, 0))), 0)::bigint
        FROM processed_summary_batches
        WHERE timezone($2, published_at)::date >= timezone($2, NOW())::date - ($1::int - 1)
    `, days, timezone).Scan(&totals.SummaryCount, &totals.ChatCount, &totals.PromptTokens, &totals.CachedPromptTokens, &totals.CompletionTokens, &totals.AvgLatencyMs); err != nil {
		return LLMUsageTotals{}, fmt.Errorf("select ranged llm usage: %w", err)
	}
	return totals, nil
}

func (r *Repository) DailyLLMUsage(ctx context.Context, days int, timezone string) ([]DailyLLMUsage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if days <= 0 {
		days = 7
	}
	if timezone == "" {
		timezone = "UTC"
	}

	rows, err := r.pool.Query(ctx, `
        WITH day_series AS (
            SELECT generate_series(
                (timezone($2, NOW())::date - ($1::int - 1)),
                timezone($2, NOW())::date,
                INTERVAL '1 day'
            )::date AS day
        ), usage_by_day AS (
            SELECT
                timezone($2, published_at)::date AS day,
                COUNT(*)::int AS summary_count,
                COUNT(DISTINCT peer_id)::int AS chat_count,
                COALESCE(SUM(llm_prompt_tokens), 0)::bigint AS prompt_tokens,
                COALESCE(SUM(llm_cached_prompt_tokens), 0)::bigint AS cached_prompt_tokens,
                COALESCE(SUM(llm_completion_tokens), 0)::bigint AS completion_tokens,
                COALESCE(ROUND(AVG(NULLIF(llm_latency_ms, 0))), 0)::bigint AS avg_latency_ms
            FROM processed_summary_batches
            WHERE timezone($2, published_at)::date >= timezone($2, NOW())::date - ($1::int - 1)
            GROUP BY 1
        )
        SELECT
            day_series.day::text,
            COALESCE(usage_by_day.summary_count, 0),
            COALESCE(usage_by_day.chat_count, 0),
            COALESCE(usage_by_day.prompt_tokens, 0),
            COALESCE(usage_by_day.cached_prompt_tokens, 0),
            COALESCE(usage_by_day.completion_tokens, 0),
            COALESCE(usage_by_day.avg_latency_ms, 0)
        FROM day_series
        LEFT JOIN usage_by_day USING (day)
        ORDER BY day_series.day DESC
    `, days, timezone)
	if err != nil {
		return nil, fmt.Errorf("select daily llm usage: %w", err)
	}
	defer rows.Close()

	stats := make([]DailyLLMUsage, 0, days)
	for rows.Next() {
		var stat DailyLLMUsage
		if err := rows.Scan(&stat.Day, &stat.SummaryCount, &stat.ChatCount, &stat.PromptTokens, &stat.CachedPromptTokens, &stat.CompletionTokens, &stat.AvgLatencyMs); err != nil {
			return nil, fmt.Errorf("scan daily llm usage: %w", err)
		}
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily llm usage: %w", err)
	}
	return stats, nil
}

func (r *Repository) ImageUsageDays(ctx context.Context, days int, timezone string) (ImageUsageTotals, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if days <= 0 {
		days = 30
	}
	if timezone == "" {
		timezone = "UTC"
	}

	var totals ImageUsageTotals
	if err := r.pool.QueryRow(ctx, `
        SELECT
            COUNT(*) FILTER (WHERE image_published)::int,
            COUNT(DISTINCT peer_id) FILTER (WHERE image_published)::int,
            COALESCE(SUM(image_prompt_llm_prompt_tokens), 0)::bigint,
            COALESCE(SUM(image_prompt_llm_cached_prompt_tokens), 0)::bigint,
            COALESCE(SUM(image_prompt_llm_completion_tokens), 0)::bigint,
            COALESCE(SUM(image_input_tokens), 0)::bigint,
            COALESCE(SUM(image_input_text_tokens), 0)::bigint,
            COALESCE(SUM(image_input_image_tokens), 0)::bigint,
            COALESCE(SUM(image_output_tokens), 0)::bigint,
            COALESCE(ROUND(AVG(NULLIF(image_prompt_llm_latency_ms, 0))), 0)::bigint,
            COALESCE(ROUND(AVG(NULLIF(image_latency_ms, 0))), 0)::bigint
        FROM processed_summary_batches
        WHERE timezone($2, published_at)::date >= timezone($2, NOW())::date - ($1::int - 1)
    `, days, timezone).Scan(&totals.ImageCount, &totals.ChatCount, &totals.PromptLLMPromptTokens, &totals.PromptLLMCachedPromptTokens, &totals.PromptLLMCompletionTokens, &totals.ImageInputTokens, &totals.ImageInputTextTokens, &totals.ImageInputImageTokens, &totals.ImageOutputTokens, &totals.AvgPromptLLMLatencyMs, &totals.AvgImageLatencyMs); err != nil {
		return ImageUsageTotals{}, fmt.Errorf("select ranged image usage: %w", err)
	}
	return totals, nil
}

func (r *Repository) DailyImageUsage(ctx context.Context, days int, timezone string) ([]DailyImageUsage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if days <= 0 {
		days = 7
	}
	if timezone == "" {
		timezone = "UTC"
	}

	rows, err := r.pool.Query(ctx, `
        WITH day_series AS (
            SELECT generate_series(
                (timezone($2, NOW())::date - ($1::int - 1)),
                timezone($2, NOW())::date,
                INTERVAL '1 day'
            )::date AS day
        ), usage_by_day AS (
            SELECT
                timezone($2, published_at)::date AS day,
                COUNT(*) FILTER (WHERE image_published)::int AS image_count,
                COUNT(DISTINCT peer_id) FILTER (WHERE image_published)::int AS chat_count,
                COALESCE(SUM(image_prompt_llm_prompt_tokens), 0)::bigint AS prompt_llm_prompt_tokens,
                COALESCE(SUM(image_prompt_llm_cached_prompt_tokens), 0)::bigint AS prompt_llm_cached_prompt_tokens,
                COALESCE(SUM(image_prompt_llm_completion_tokens), 0)::bigint AS prompt_llm_completion_tokens,
                COALESCE(SUM(image_input_tokens), 0)::bigint AS image_input_tokens,
                COALESCE(SUM(image_input_text_tokens), 0)::bigint AS image_input_text_tokens,
                COALESCE(SUM(image_input_image_tokens), 0)::bigint AS image_input_image_tokens,
                COALESCE(SUM(image_output_tokens), 0)::bigint AS image_output_tokens,
                COALESCE(ROUND(AVG(NULLIF(image_prompt_llm_latency_ms, 0))), 0)::bigint AS avg_prompt_llm_latency_ms,
                COALESCE(ROUND(AVG(NULLIF(image_latency_ms, 0))), 0)::bigint AS avg_image_latency_ms
            FROM processed_summary_batches
            WHERE timezone($2, published_at)::date >= timezone($2, NOW())::date - ($1::int - 1)
            GROUP BY 1
        )
        SELECT
            day_series.day::text,
            COALESCE(usage_by_day.image_count, 0),
            COALESCE(usage_by_day.chat_count, 0),
            COALESCE(usage_by_day.prompt_llm_prompt_tokens, 0),
            COALESCE(usage_by_day.prompt_llm_cached_prompt_tokens, 0),
            COALESCE(usage_by_day.prompt_llm_completion_tokens, 0),
            COALESCE(usage_by_day.image_input_tokens, 0),
            COALESCE(usage_by_day.image_input_text_tokens, 0),
            COALESCE(usage_by_day.image_input_image_tokens, 0),
            COALESCE(usage_by_day.image_output_tokens, 0),
            COALESCE(usage_by_day.avg_prompt_llm_latency_ms, 0),
            COALESCE(usage_by_day.avg_image_latency_ms, 0)
        FROM day_series
        LEFT JOIN usage_by_day USING (day)
        ORDER BY day_series.day DESC
    `, days, timezone)
	if err != nil {
		return nil, fmt.Errorf("select daily image usage: %w", err)
	}
	defer rows.Close()

	stats := make([]DailyImageUsage, 0, days)
	for rows.Next() {
		var stat DailyImageUsage
		if err := rows.Scan(&stat.Day, &stat.ImageCount, &stat.ChatCount, &stat.PromptLLMPromptTokens, &stat.PromptLLMCachedPromptTokens, &stat.PromptLLMCompletionTokens, &stat.ImageInputTokens, &stat.ImageInputTextTokens, &stat.ImageInputImageTokens, &stat.ImageOutputTokens, &stat.AvgPromptLLMLatencyMs, &stat.AvgImageLatencyMs); err != nil {
			return nil, fmt.Errorf("scan daily image usage: %w", err)
		}
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily image usage: %w", err)
	}
	return stats, nil
}
