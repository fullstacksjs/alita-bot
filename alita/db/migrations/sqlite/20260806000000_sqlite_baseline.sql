-- SQLite Baseline Migration for Retained Domain Concepts

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id BIGINT NOT NULL,
    username TEXT DEFAULT '',
    name TEXT DEFAULT '',
    last_activity DATETIME,
    created_at DATETIME,
    updated_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_user_id ON users (user_id);
CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);

-- Chats table
CREATE TABLE IF NOT EXISTS chats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    chat_name TEXT DEFAULT '',
    users TEXT DEFAULT '[]',
    is_inactive BOOLEAN DEFAULT 0,
    last_activity DATETIME,
    created_at DATETIME,
    updated_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_chat_id ON chats (chat_id);

-- Antiflood Settings table
CREATE TABLE IF NOT EXISTS antiflood_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    flood_limit INTEGER DEFAULT 5,
    action TEXT DEFAULT 'mute',
    delete_antiflood_message BOOLEAN DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_antiflood_settings_chat_id ON antiflood_settings (chat_id);

-- AntiRaid Settings table
CREATE TABLE IF NOT EXISTS antiraid_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    raid_time INTEGER DEFAULT 21600,
    raid_action_time INTEGER DEFAULT 3600,
    auto_antiraid_threshold INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_antiraid_settings_chat_id ON antiraid_settings (chat_id);

-- Approved Users table
CREATE TABLE IF NOT EXISTS approved_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id BIGINT NOT NULL,
    chat_id BIGINT NOT NULL,
    reason TEXT DEFAULT '',
    approved_by BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users (user_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_approved_users_chat_user ON approved_users (chat_id, user_id);

-- Blacklist Settings table
CREATE TABLE IF NOT EXISTS blacklists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    word TEXT NOT NULL,
    action TEXT DEFAULT 'warn',
    reason TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_blacklist_chat_word ON blacklists (chat_id, word);

-- Channel Settings table
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    channel_id BIGINT,
    channel_name TEXT,
    username TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_chat_id ON channels (chat_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_username ON channels (LOWER(username)) WHERE username IS NOT NULL AND username <> '';

-- Connection Settings table
CREATE TABLE IF NOT EXISTS connection (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id BIGINT NOT NULL,
    chat_id BIGINT NOT NULL,
    connected BOOLEAN DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users (user_id) ON DELETE CASCADE,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_connection_user_id ON connection (user_id);

-- Chat Filters table
CREATE TABLE IF NOT EXISTS filters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    keyword TEXT NOT NULL,
    filter_reply TEXT,
    msgtype INTEGER,
    fileid TEXT,
    nonotif BOOLEAN DEFAULT 0,
    filter_buttons TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_filters_chat_keyword ON filters (chat_id, keyword);

-- Greetings table
CREATE TABLE IF NOT EXISTS greetings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    clean_service_settings BOOLEAN DEFAULT 0,
    welcome_clean_old BOOLEAN DEFAULT 0,
    welcome_last_msg_id BIGINT,
    welcome_enabled BOOLEAN DEFAULT 1,
    welcome_text TEXT,
    welcome_file_id TEXT,
    welcome_type INTEGER DEFAULT 1,
    welcome_btns TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_greetings_chat_id ON greetings (chat_id);

-- Notes Settings table
CREATE TABLE IF NOT EXISTS notes_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    private BOOLEAN DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_settings_chat_id ON notes_settings (chat_id);

-- Notes table
CREATE TABLE IF NOT EXISTS notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    note_name TEXT NOT NULL,
    note_content TEXT,
    file_id TEXT,
    msg_type INTEGER,
    buttons TEXT,
    admin_only BOOLEAN DEFAULT 0,
    private_only BOOLEAN DEFAULT 0,
    group_only BOOLEAN DEFAULT 0,
    web_preview BOOLEAN DEFAULT 1,
    is_protected BOOLEAN DEFAULT 0,
    no_notif BOOLEAN DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_notes_chat_name ON notes (chat_id, note_name);

-- Reactions table
CREATE TABLE IF NOT EXISTS reactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    keyword TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reactions_chat_keyword ON reactions (chat_id, keyword);

-- Warn Settings table
CREATE TABLE IF NOT EXISTS warns_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    warn_limit INTEGER DEFAULT 3,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_warns_settings_chat_id ON warns_settings (chat_id);

-- Warn Events table
CREATE TABLE IF NOT EXISTS warn_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id BIGINT NOT NULL,
    chat_id BIGINT NOT NULL,
    reason TEXT DEFAULT '',
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users (user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_warn_events_user_chat ON warn_events (user_id, chat_id);
