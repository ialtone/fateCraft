-- Fatecraft 数据库初始化脚本
-- 执行方式：
--   1. 先创建数据库和用户（见下方命令）
--   2. 连接到 fatecraft 数据库后执行本脚本

-- =============================================================================
-- 数据库创建命令（PostgreSQL 管理员执行）
-- =============================================================================
-- CREATE DATABASE fatecraft WITH ENCODING='UTF8' LC_COLLATE='zh_CN.UTF-8' LC_CTYPE='zh_CN.UTF-8' TEMPLATE=template0;
-- CREATE USER fatecraft WITH PASSWORD 'fatecraft-dev';
-- GRANT ALL PRIVILEGES ON DATABASE fatecraft TO fatecraft;
-- \c fatecraft
-- GRANT ALL ON SCHEMA public TO fatecraft;

-- =============================================================================
-- 表结构
-- =============================================================================

-- 对话表
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL,
    title VARCHAR(256) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    is_processing BOOLEAN DEFAULT FALSE,
    character_id VARCHAR(64),
    persona_id VARCHAR(64),
    scenario_id VARCHAR(64),
    prompt_preset_id VARCHAR(64),
    rewinds_used_this_trial INTEGER NOT NULL DEFAULT 0,
    roll_system_enabled BOOLEAN,
    roll_difficulty_mode VARCHAR(10),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMP WITH TIME ZONE,
    archived_at TIMESTAMP WITH TIME ZONE,
    deleted_at TIMESTAMP WITH TIME ZONE
);

COMMENT ON COLUMN conversations.rewinds_used_this_trial IS '本次轮回使用的天道回溯次数';
COMMENT ON COLUMN conversations.roll_system_enabled IS '此对话是否启用判定系统（null=使用全局设置）';
COMMENT ON COLUMN conversations.roll_difficulty_mode IS '此对话的判定难度模式（null=使用全局设置）';

CREATE INDEX IF NOT EXISTS idx_conversations_player_id ON conversations(player_id);
CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations(status);
CREATE INDEX IF NOT EXISTS idx_conversations_player_updated ON conversations(player_id, updated_at DESC);

-- 对话世界书关联表
CREATE TABLE IF NOT EXISTS conversation_world_books (
    conversation_id UUID NOT NULL,
    world_book_id VARCHAR(64) NOT NULL,
    PRIMARY KEY (conversation_id, world_book_id),
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_conversation_world_books_conversation ON conversation_world_books(conversation_id);

-- 聊天消息表（内部消息，用于上下文）
CREATE TABLE IF NOT EXISTS chat_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL,
    role VARCHAR(16) NOT NULL,
    content TEXT NOT NULL,
    hidden BOOLEAN DEFAULT FALSE,
    token_count INTEGER,
    parent_message_id UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation ON chat_messages(conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chat_messages_role ON chat_messages(role);

-- 展示消息表（UI 消息）
CREATE TABLE IF NOT EXISTS display_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL,
    chat_message_id UUID,
    type VARCHAR(16) NOT NULL,
    markdown TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (chat_message_id) REFERENCES chat_messages(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_display_messages_conversation ON display_messages(conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_display_messages_type ON display_messages(type);

-- 对话状态表
CREATE TABLE IF NOT EXISTS conversation_states (
    conversation_id UUID PRIMARY KEY,
    state JSONB NOT NULL DEFAULT '{}',
    schema_version INTEGER DEFAULT 1,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

-- 状态变更历史表
CREATE TABLE IF NOT EXISTS state_revisions (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL,
    source VARCHAR(16) NOT NULL,
    related_message_id UUID,
    patch JSONB NOT NULL,
    before_state JSONB NOT NULL,
    after_state JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (related_message_id) REFERENCES chat_messages(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_state_revisions_conversation ON state_revisions(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_state_revisions_source ON state_revisions(source);

-- 记忆摘要表
CREATE TABLE IF NOT EXISTS memory_summaries (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    covered_until_message_id UUID,
    token_count INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (covered_until_message_id) REFERENCES chat_messages(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_memory_summaries_conversation ON memory_summaries(conversation_id, version DESC);

-- 上下文构建日志表
CREATE TABLE IF NOT EXISTS context_build_logs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL,
    trigger_message_id UUID,
    model VARCHAR(64) NOT NULL,
    max_context_tokens INTEGER NOT NULL,
    reserved_response_tokens INTEGER NOT NULL,
    used_prompt_tokens INTEGER NOT NULL,
    segments JSONB NOT NULL,
    included_message_ids TEXT[] DEFAULT '{}',
    activated_world_entry_ids TEXT[] DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (trigger_message_id) REFERENCES chat_messages(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_context_build_logs_conversation ON context_build_logs(conversation_id, created_at DESC);

-- LLM 设置表（单行配置）
CREATE TABLE IF NOT EXISTS llm_settings (
    id INTEGER PRIMARY KEY DEFAULT 1,
    base_url VARCHAR(256) NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key TEXT,
    model VARCHAR(64) NOT NULL DEFAULT '',
    max_context_tokens INTEGER NOT NULL DEFAULT 32000,
    max_response_tokens INTEGER NOT NULL DEFAULT 4096,
    temperature DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    top_p DOUBLE PRECISION NOT NULL DEFAULT 0.95,
    stream_enabled BOOLEAN DEFAULT TRUE,
    roll_system_enabled BOOLEAN DEFAULT FALSE,
    roll_difficulty_mode VARCHAR(10) DEFAULT 'normal',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT llm_settings_singleton CHECK (id = 1)
);

COMMENT ON COLUMN llm_settings.roll_system_enabled IS '是否启用天命判定系统';
COMMENT ON COLUMN llm_settings.roll_difficulty_mode IS '判定难度模式：easy(简单+15%) / normal(普通) / hard(困难-15%)';

-- 插入默认设置
INSERT INTO llm_settings (id, base_url, model, max_context_tokens, max_response_tokens, temperature, top_p, stream_enabled, roll_system_enabled, roll_difficulty_mode)
VALUES (1, 'https://api.openai.com/v1', '', 32000, 4096, 0.8, 0.95, TRUE, FALSE, 'normal')
ON CONFLICT (id) DO NOTHING;

-- 账号表（天道回溯系统 + 灵石统计）
CREATE TABLE IF NOT EXISTS accounts (
    id SERIAL PRIMARY KEY,
    player_id VARCHAR(255) NOT NULL UNIQUE DEFAULT 'default_player',
    rewind_count INTEGER NOT NULL DEFAULT 10,
    total_rewinds_earned INTEGER NOT NULL DEFAULT 10,
    rewinds_used INTEGER NOT NULL DEFAULT 0,
    total_spirit_stones BIGINT NOT NULL DEFAULT 0,
    successful_trials INTEGER NOT NULL DEFAULT 0,
    failed_trials INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE accounts IS '账号全局数据';
COMMENT ON COLUMN accounts.rewind_count IS '当前可用的天道回溯次数';
COMMENT ON COLUMN accounts.total_rewinds_earned IS '历史总获得的回溯次数';
COMMENT ON COLUMN accounts.rewinds_used IS '已使用的回溯次数';
COMMENT ON COLUMN accounts.total_spirit_stones IS '历史累计获得的灵石总数（破碎虚空带出）';
COMMENT ON COLUMN accounts.successful_trials IS '成功轮回次数（破碎虚空）';
COMMENT ON COLUMN accounts.failed_trials IS '失败轮回次数（道消身殒）';

-- 初始化默认账号
INSERT INTO accounts (player_id, rewind_count, total_rewinds_earned, rewinds_used, total_spirit_stones, successful_trials, failed_trials)
VALUES ('default_player', 10, 10, 0, 0, 0, 0)
ON CONFLICT (player_id) DO NOTHING;

-- 轮回历史记录表
CREATE TABLE IF NOT EXISTS trial_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id VARCHAR(255) NOT NULL,
    conversation_id UUID NOT NULL,
    ending_type VARCHAR(16) NOT NULL,
    spirit_stones_earned BIGINT NOT NULL DEFAULT 0,
    character_name VARCHAR(128),
    cultivation_level VARCHAR(64),
    survival_turns INTEGER,
    ended_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

COMMENT ON TABLE trial_history IS '轮回历史记录';
COMMENT ON COLUMN trial_history.ending_type IS '结局类型：good(破碎虚空) / bad(道消身殒)';
COMMENT ON COLUMN trial_history.spirit_stones_earned IS '本次轮回带出的灵石数（失败为0）';
COMMENT ON COLUMN trial_history.character_name IS '角色名';
COMMENT ON COLUMN trial_history.cultivation_level IS '最终修为境界';
COMMENT ON COLUMN trial_history.survival_turns IS '存活回合数';

CREATE INDEX IF NOT EXISTS idx_trial_history_player ON trial_history(player_id, ended_at DESC);
CREATE INDEX IF NOT EXISTS idx_trial_history_conversation ON trial_history(conversation_id);

-- 天命判定历史表（持久化连败补偿/连胜惩罚等历史调整所需的判定流水，重启不丢）
CREATE TABLE IF NOT EXISTS roll_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL,
    roll_type VARCHAR(64) NOT NULL,
    success BOOLEAN NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);

COMMENT ON TABLE roll_history IS '天命判定流水，用于历史调整（连败补偿、同类重试、连胜惩罚）';
COMMENT ON COLUMN roll_history.roll_type IS '判定类型（如破阵判定），用于同类重试匹配';

CREATE INDEX IF NOT EXISTS idx_roll_history_conversation ON roll_history(conversation_id, created_at DESC);

-- =============================================================================
-- 初始化完成
-- =============================================================================
