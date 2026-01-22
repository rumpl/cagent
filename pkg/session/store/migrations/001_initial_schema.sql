-- +goose Up

PRAGMA foreign_keys = ON;

-- =============================================================================
-- SESSIONS
-- =============================================================================

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tools_approved INTEGER NOT NULL DEFAULT 0,
    thinking INTEGER NOT NULL DEFAULT 0,
    hide_tool_results INTEGER NOT NULL DEFAULT 0,
    working_dir TEXT,
    max_iterations INTEGER NOT NULL DEFAULT 0,
    starred INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0.0,
    -- PermissionsConfig as JSON
    permissions TEXT,
    -- map[string]string as JSON
    agent_model_overrides TEXT,
    -- []string as JSON
    custom_models_used TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_starred ON sessions(starred) WHERE starred = 1;

-- =============================================================================
-- SESSION ITEMS ([]Item)
-- =============================================================================

CREATE TABLE IF NOT EXISTS session_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    item_type TEXT NOT NULL CHECK(item_type IN ('message', 'sub_session', 'summary')),
    sub_session_id TEXT,
    summary TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (sub_session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_session_items_session_position ON session_items(session_id, position);

-- =============================================================================
-- MESSAGES (Message + chat.Message merged, 1:1 with session_item)
-- =============================================================================

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_item_id INTEGER NOT NULL UNIQUE,
    agent_name TEXT NOT NULL DEFAULT '',
    implicit INTEGER NOT NULL DEFAULT 0,
    -- chat.Message fields
    role TEXT NOT NULL CHECK(role IN ('system', 'user', 'assistant', 'tool')),
    content TEXT NOT NULL DEFAULT '',
    reasoning_content TEXT,
    thinking_signature TEXT,
    thought_signature BLOB,
    tool_call_id TEXT,
    created_at TEXT,
    model TEXT,
    cost REAL NOT NULL DEFAULT 0.0,
    cache_control INTEGER NOT NULL DEFAULT 0,
    -- chat.Usage fields (1:1, nullable - only for assistant messages)
    usage_input_tokens INTEGER,
    usage_output_tokens INTEGER,
    usage_cached_input_tokens INTEGER,
    usage_cache_write_tokens INTEGER,
    usage_reasoning_tokens INTEGER,
    -- FunctionCall fields (1:1, nullable)
    function_call_name TEXT,
    function_call_arguments TEXT,
    FOREIGN KEY (session_item_id) REFERENCES session_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_role ON messages(role);
CREATE INDEX IF NOT EXISTS idx_messages_tool_call_id ON messages(tool_call_id) WHERE tool_call_id IS NOT NULL;

-- =============================================================================
-- MESSAGE ARRAYS
-- =============================================================================

-- MultiContent ([]MessagePart with embedded ImageURL)
CREATE TABLE IF NOT EXISTS message_parts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    part_type TEXT NOT NULL CHECK(part_type IN ('text', 'image_url')),
    text TEXT,
    image_url TEXT,
    image_url_detail TEXT CHECK(image_url_detail IN ('high', 'low', 'auto')),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_message_parts_message ON message_parts(message_id, position);

-- ToolCalls ([]tools.ToolCall with embedded FunctionCall)
CREATE TABLE IF NOT EXISTS message_tool_calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    tool_call_id TEXT,
    tool_type TEXT,
    function_name TEXT,
    function_arguments TEXT,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_message_tool_calls_message ON message_tool_calls(message_id, position);

-- ToolDefinitions ([]tools.Tool with embedded Annotations)
CREATE TABLE IF NOT EXISTS message_tool_definitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    name TEXT NOT NULL,
    category TEXT,
    description TEXT,
    parameters_schema TEXT,
    output_schema TEXT,
    annotation_title TEXT,
    annotation_read_only_hint INTEGER,
    annotation_destructive_hint INTEGER,
    annotation_idempotent_hint INTEGER,
    annotation_open_world_hint INTEGER,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_message_tool_definitions_message ON message_tool_definitions(message_id, position);

-- +goose Down
DROP TABLE IF EXISTS message_tool_definitions;
DROP TABLE IF EXISTS message_tool_calls;
DROP TABLE IF EXISTS message_parts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS session_items;
DROP TABLE IF EXISTS sessions;
