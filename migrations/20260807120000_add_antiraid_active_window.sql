-- Persist the anti-raid active window so the expiry worker recovers in-flight
-- raids after a restart. Both columns are runtime state, not configuration:
-- NULL means no raid is open for the chat.
ALTER TABLE antiraid_settings
    ADD COLUMN IF NOT EXISTS raid_started_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS raid_active_until TIMESTAMP WITH TIME ZONE;

-- The expiry sweep scans for elapsed windows across every chat.
CREATE INDEX IF NOT EXISTS idx_antiraid_settings_active_until
    ON antiraid_settings (raid_active_until);
