ALTER TABLE warns_settings DROP CONSTRAINT IF EXISTS chk_warn_mode;
ALTER TABLE warns_settings DROP CONSTRAINT IF EXISTS chk_warns_mode;
ALTER TABLE warns_settings DROP COLUMN IF EXISTS warn_mode;

ALTER TABLE greetings DROP COLUMN IF EXISTS auto_approve;
