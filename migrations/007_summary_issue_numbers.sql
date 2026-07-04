CREATE TABLE IF NOT EXISTS summary_issue_counters (
    peer_id BIGINT PRIMARY KEY,
    next_issue_number BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE processed_summary_batches
    ADD COLUMN IF NOT EXISTS issue_number BIGINT;

WITH numbered AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY peer_id ORDER BY published_at ASC, id ASC) AS issue_number
    FROM processed_summary_batches
    WHERE issue_number IS NULL
)
UPDATE processed_summary_batches AS batches
SET issue_number = numbered.issue_number
FROM numbered
WHERE batches.id = numbered.id;

INSERT INTO summary_issue_counters (peer_id, next_issue_number, updated_at)
SELECT peer_id, COALESCE(MAX(issue_number), 0) + 1, NOW()
FROM processed_summary_batches
GROUP BY peer_id
ON CONFLICT (peer_id) DO UPDATE SET
    next_issue_number = GREATEST(summary_issue_counters.next_issue_number, EXCLUDED.next_issue_number),
    updated_at = NOW();

CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_summary_batches_peer_issue
    ON processed_summary_batches (peer_id, issue_number)
    WHERE issue_number IS NOT NULL;
