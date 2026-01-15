-- +goose Up
-- Core session metadata
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    working_dir TEXT DEFAULT '',
    created_at TEXT NOT NULL,
    starred INTEGER NOT NULL DEFAULT 0,
    tools_approved INTEGER NOT NULL DEFAULT 0,
    send_user_message INTEGER NOT NULL DEFAULT 1,
    max_iterations INTEGER NOT NULL DEFAULT 0,
    
    -- Aggregated usage (denormalized for quick access)
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0,
    
    -- Parent session for sub-sessions (NULL for root sessions)
    parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    parent_item_order INTEGER,
    
    -- Permissions stored as JSON (relatively small, rarely queried)
    permissions TEXT DEFAULT ''
);

CREATE INDEX idx_sessions_created_at ON sessions(created_at DESC);
CREATE INDEX idx_sessions_starred ON sessions(starred);
CREATE INDEX idx_sessions_parent ON sessions(parent_session_id);

-- Session items (messages or references to sub-sessions)
CREATE TABLE session_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    item_order INTEGER NOT NULL,
    item_type TEXT NOT NULL CHECK (item_type IN ('message', 'sub_session', 'summary')),
    
    -- For sub_session type, references the child session
    sub_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    
    -- For summary type
    summary_text TEXT,
    
    UNIQUE(session_id, item_order)
);

CREATE INDEX idx_session_items_session ON session_items(session_id, item_order);

-- Messages table
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_item_id INTEGER NOT NULL UNIQUE REFERENCES session_items(id) ON DELETE CASCADE,
    
    agent_name TEXT,
    role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    implicit INTEGER NOT NULL DEFAULT 0,
    
    -- For tool role messages
    tool_call_id TEXT,
    
    -- For assistant messages
    model TEXT,
    reasoning_content TEXT,
    thinking_signature TEXT,
    thought_signature BLOB,
    
    -- Per-message cost (optional)
    message_cost REAL DEFAULT 0,
    
    -- Usage (primarily for assistant messages)
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cached_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_messages_role ON messages(role);
CREATE INDEX idx_messages_agent ON messages(agent_name);
CREATE INDEX idx_messages_created_at ON messages(created_at);

-- Multi-modal content parts
CREATE TABLE message_parts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    part_order INTEGER NOT NULL,
    part_type TEXT NOT NULL CHECK (part_type IN ('text', 'image_url')),
    text_content TEXT,
    image_url TEXT,
    image_detail TEXT CHECK (image_detail IS NULL OR image_detail IN ('high', 'low', 'auto')),
    
    UNIQUE(message_id, part_order)
);

CREATE INDEX idx_message_parts_message ON message_parts(message_id);

-- Tool calls (from assistant messages)
CREATE TABLE tool_calls (
    id TEXT PRIMARY KEY,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    call_order INTEGER NOT NULL,
    tool_type TEXT NOT NULL DEFAULT 'function',
    function_name TEXT NOT NULL,
    function_arguments TEXT NOT NULL,
    
    UNIQUE(message_id, call_order)
);

CREATE INDEX idx_tool_calls_message ON tool_calls(message_id);
CREATE INDEX idx_tool_calls_function ON tool_calls(function_name);

-- Tool definitions (referenced by tool calls, deduplicated by content hash)
CREATE TABLE tool_definitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    category TEXT,
    description TEXT,
    parameters TEXT,
    annotations TEXT,
    output_schema TEXT,
    content_hash TEXT UNIQUE NOT NULL
);

CREATE INDEX idx_tool_definitions_name ON tool_definitions(name);

-- Junction table: which tool definitions are referenced in which message
CREATE TABLE message_tool_definitions (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_definition_id INTEGER NOT NULL REFERENCES tool_definitions(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, tool_definition_id)
);

-- Agent model overrides per session
CREATE TABLE session_model_overrides (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    model_reference TEXT NOT NULL,
    PRIMARY KEY (session_id, agent_name)
);

-- Custom models used in session
CREATE TABLE session_custom_models (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    model_reference TEXT NOT NULL,
    PRIMARY KEY (session_id, model_reference)
);

-- +goose Down
DROP TABLE IF EXISTS session_custom_models;
DROP TABLE IF EXISTS session_model_overrides;
DROP TABLE IF EXISTS message_tool_definitions;
DROP TABLE IF EXISTS tool_definitions;
DROP TABLE IF EXISTS tool_calls;
DROP TABLE IF EXISTS message_parts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS session_items;
DROP TABLE IF EXISTS sessions;
