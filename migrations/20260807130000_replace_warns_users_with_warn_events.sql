-- Replace the aggregated warns_users row with one row per warning event.
--
-- The model and the SQLite baseline moved to warn_events, where the warning
-- count is the row count rather than a stored num_warns column. This brings the
-- PostgreSQL schema to the same shape and carries the existing warnings over.

CREATE TABLE IF NOT EXISTS warn_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    chat_id BIGINT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_warn_events_user_chat ON warn_events (user_id, chat_id);

ALTER TABLE warn_events DROP CONSTRAINT IF EXISTS fk_warn_events_chat;
ALTER TABLE warn_events
    ADD CONSTRAINT fk_warn_events_chat
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE warn_events DROP CONSTRAINT IF EXISTS fk_warn_events_user;
ALTER TABLE warn_events
    ADD CONSTRAINT fk_warn_events_user
    FOREIGN KEY (user_id) REFERENCES users (user_id) ON DELETE CASCADE ON UPDATE CASCADE;

-- Expand each stored reason into its own event. warns is jsonb; anything that
-- is not an array is treated as no stored reasons.
INSERT INTO warn_events (user_id, chat_id, reason, created_at, updated_at)
SELECT
    wu.user_id,
    wu.chat_id,
    r.reason,
    COALESCE(wu.created_at, NOW()),
    COALESCE(wu.updated_at, NOW())
FROM warns_users AS wu
CROSS JOIN LATERAL jsonb_array_elements_text(
    CASE WHEN jsonb_typeof(wu.warns) = 'array' THEN wu.warns ELSE '[]'::jsonb END
) AS r(reason);

-- Older rows can carry a num_warns larger than the number of stored reasons.
-- Pad those so the migrated warning count matches what the chat had.
INSERT INTO warn_events (user_id, chat_id, reason, created_at, updated_at)
SELECT
    wu.user_id,
    wu.chat_id,
    'No Reason',
    COALESCE(wu.created_at, NOW()),
    COALESCE(wu.updated_at, NOW())
FROM warns_users AS wu
CROSS JOIN LATERAL generate_series(
    1,
    GREATEST(
        COALESCE(wu.num_warns, 0)::int
            - COALESCE(
                jsonb_array_length(
                    CASE WHEN jsonb_typeof(wu.warns) = 'array' THEN wu.warns ELSE '[]'::jsonb END
                ),
                0
            ),
        0
    )
) AS pad;

DROP TABLE IF EXISTS warns_users;
