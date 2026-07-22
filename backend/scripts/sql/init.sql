-- Fatecraft PostgreSQL schema (fresh database only)
-- 在空的 fatecraft 数据库中执行本文件；本项目不维护旧结构兼容迁移。

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(320) UNIQUE,
    password_hash TEXT,
    display_name VARCHAR(128) NOT NULL,
    avatar_url TEXT,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    role VARCHAR(16) NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_users_status ON users(status);
CREATE INDEX idx_users_role ON users(role);

CREATE TABLE auth_identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    provider_user_id VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    provider_email VARCHAR(320),
    trust_level INTEGER,
    profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider, provider_user_id)
);
CREATE INDEX idx_auth_identities_user ON auth_identities(user_id);

CREATE TABLE auth_sessions (
    token_hash CHAR(64) PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX idx_auth_sessions_expires ON auth_sessions(expires_at);

CREATE TABLE oauth_login_flows (
    state_hash CHAR(64) PRIMARY KEY,
    provider VARCHAR(32) NOT NULL,
    code_verifier VARCHAR(128) NOT NULL,
    return_to TEXT NOT NULL DEFAULT '/',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_oauth_login_flows_expires ON oauth_login_flows(expires_at);

CREATE TABLE auth_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    email_auth_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    email_registration_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    email_verification_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    invitation_required BOOLEAN NOT NULL DEFAULT FALSE,
    smtp_host TEXT NOT NULL DEFAULT '',
    smtp_port INTEGER NOT NULL DEFAULT 587 CHECK (smtp_port BETWEEN 1 AND 65535),
    smtp_security VARCHAR(16) NOT NULL DEFAULT 'starttls' CHECK (smtp_security IN ('none', 'starttls', 'tls')),
    smtp_username TEXT NOT NULL DEFAULT '',
    smtp_password_encrypted TEXT NOT NULL DEFAULT '',
    sender_email VARCHAR(320) NOT NULL DEFAULT '',
    sender_name VARCHAR(128) NOT NULL DEFAULT 'Fatecraft',
    verification_subject TEXT NOT NULL DEFAULT '【Fatecraft】邮箱验证码',
    verification_template TEXT NOT NULL DEFAULT E'你的验证码是：{{code}}\n\n验证码将在 {{expires_minutes}} 分钟后失效。如非本人操作，请忽略此邮件。',
    verification_code_ttl_minutes INTEGER NOT NULL DEFAULT 10 CHECK (verification_code_ttl_minutes BETWEEN 1 AND 60),
    verification_resend_seconds INTEGER NOT NULL DEFAULT 60 CHECK (verification_resend_seconds BETWEEN 30 AND 3600),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO auth_settings (id) VALUES (1);

CREATE TABLE email_verification_codes (
    id UUID PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    purpose VARCHAR(32) NOT NULL CHECK (purpose IN ('registration')),
    code_hash TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_email_verification_lookup ON email_verification_codes(email, purpose, created_at DESC);
CREATE UNIQUE INDEX idx_email_verification_active ON email_verification_codes(email, purpose) WHERE consumed_at IS NULL;
CREATE INDEX idx_email_verification_expires ON email_verification_codes(expires_at);

CREATE TABLE invitation_codes (
    id UUID PRIMARY KEY,
    code_hash CHAR(64) NOT NULL UNIQUE,
    code_hint VARCHAR(8) NOT NULL,
    label VARCHAR(128) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_uses INTEGER NOT NULL DEFAULT 1 CHECK (max_uses >= 0),
    used_count INTEGER NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    expires_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_invitation_codes_status ON invitation_codes(enabled, expires_at);

-- 挑战是运行时内容的聚合根。current_version_id 的外键在版本表创建后补上。
CREATE TABLE challenge_packs (
    id UUID PRIMARY KEY,
    slug VARCHAR(128) NOT NULL UNIQUE,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    cover_url TEXT,
    status VARCHAR(16) NOT NULL DEFAULT 'hidden' CHECK (status IN ('active', 'hidden', 'retired')),
    current_version_id UUID,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE challenge_versions (
    id UUID PRIMARY KEY,
    challenge_pack_id UUID NOT NULL REFERENCES challenge_packs(id) ON DELETE RESTRICT,
    version VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
    manifest JSONB NOT NULL,
    content_hash CHAR(64) NOT NULL DEFAULT '',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    published_by UUID REFERENCES users(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (challenge_pack_id, version),
    UNIQUE (id, challenge_pack_id),
    CONSTRAINT challenge_version_publish_state CHECK (
        (status = 'draft' AND btrim(content_hash) = '' AND published_at IS NULL)
        OR
        (status IN ('published', 'retired') AND btrim(content_hash) ~ '^[0-9a-f]{64}$' AND published_at IS NOT NULL)
    )
);
CREATE INDEX idx_challenge_versions_pack ON challenge_versions(challenge_pack_id, created_at DESC);

ALTER TABLE challenge_packs
    ADD CONSTRAINT fk_challenge_packs_current_version
    FOREIGN KEY (current_version_id, id)
    REFERENCES challenge_versions(id, challenge_pack_id) ON DELETE RESTRICT;

CREATE TABLE challenge_assets (
    id UUID PRIMARY KEY,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('character', 'persona', 'scenario', 'prompt', 'worldbook')),
    slug VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    content JSONB NOT NULL CHECK (jsonb_typeof(content) = 'object'),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (challenge_version_id, kind, slug),
    UNIQUE (id, challenge_version_id)
);
CREATE INDEX idx_challenge_assets_version ON challenge_assets(challenge_version_id, kind, sort_order);

CREATE TABLE challenge_reward_rules (
    id UUID PRIMARY KEY,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE CASCADE,
    reward_key VARCHAR(128) NOT NULL,
    ending_type VARCHAR(16) NOT NULL CHECK (ending_type = 'good'),
    ending_title VARCHAR(128) NOT NULL,
    conditions JSONB NOT NULL CHECK (jsonb_typeof(conditions) = 'object'),
    calculation JSONB NOT NULL CHECK (jsonb_typeof(calculation) = 'object'),
    max_reward BIGINT CHECK (max_reward IS NULL OR max_reward >= 0),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (challenge_version_id, reward_key)
);
CREATE INDEX idx_challenge_reward_rules_version ON challenge_reward_rules(challenge_version_id);

CREATE TABLE conversations (
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(256) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ended', 'archived', 'deleted')),
    is_processing BOOLEAN NOT NULL DEFAULT FALSE,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    challenge_hash CHAR(64) NOT NULL,
    character_asset_id UUID,
    persona_asset_id UUID,
    scenario_asset_id UUID,
    rewinds_used_this_trial INTEGER NOT NULL DEFAULT 0 CHECK (rewinds_used_this_trial >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    UNIQUE (id, player_id, challenge_version_id),
    CONSTRAINT fk_conversations_character_version
        FOREIGN KEY (character_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT fk_conversations_persona_version
        FOREIGN KEY (persona_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT fk_conversations_scenario_version
        FOREIGN KEY (scenario_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT
);
CREATE INDEX idx_conversations_player_id ON conversations(player_id);
CREATE INDEX idx_conversations_status ON conversations(status);
CREATE INDEX idx_conversations_player_updated ON conversations(player_id, updated_at DESC);
CREATE INDEX idx_conversations_challenge_version ON conversations(challenge_version_id);

CREATE TABLE chat_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool', 'internal')),
    content TEXT NOT NULL,
    hidden BOOLEAN NOT NULL DEFAULT FALSE,
    token_count INTEGER,
    parent_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_chat_messages_conversation ON chat_messages(conversation_id, created_at);

CREATE TABLE display_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    chat_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    type VARCHAR(16) NOT NULL,
    markdown TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_display_messages_conversation ON display_messages(conversation_id, created_at);

CREATE TABLE conversation_states (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    state JSONB NOT NULL DEFAULT '{}'::jsonb,
    schema_version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE state_revisions (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    source VARCHAR(16) NOT NULL CHECK (source IN ('ai', 'system')),
    related_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    patch JSONB NOT NULL,
    before_state JSONB NOT NULL,
    after_state JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_state_revisions_conversation ON state_revisions(conversation_id, created_at DESC);

CREATE TABLE memory_summaries (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    covered_until_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    token_count INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_memory_summaries_conversation ON memory_summaries(conversation_id, version DESC);

CREATE TABLE context_build_logs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    trigger_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    challenge_hash CHAR(64) NOT NULL,
    model VARCHAR(128) NOT NULL,
    max_context_tokens INTEGER NOT NULL,
    reserved_response_tokens INTEGER NOT NULL,
    used_prompt_tokens INTEGER NOT NULL,
    segments JSONB NOT NULL,
    included_message_ids TEXT[] NOT NULL DEFAULT '{}',
    activated_world_entry_ids TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_context_build_logs_conversation ON context_build_logs(conversation_id, created_at DESC);

CREATE TABLE llm_settings (
    player_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    base_url VARCHAR(512) NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key TEXT,
    model VARCHAR(128) NOT NULL DEFAULT '',
    max_context_tokens INTEGER NOT NULL DEFAULT 32000,
    max_response_tokens INTEGER NOT NULL DEFAULT 4096,
    temperature DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    top_p DOUBLE PRECISION NOT NULL DEFAULT 0.95,
    stream_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE system_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    roll_system_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    roll_difficulty_mode VARCHAR(10) NOT NULL DEFAULT 'normal' CHECK (roll_difficulty_mode IN ('easy', 'normal', 'hard')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO system_settings (id) VALUES (1);

CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    player_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    rewind_count INTEGER NOT NULL DEFAULT 10 CHECK (rewind_count >= 0),
    total_rewinds_earned INTEGER NOT NULL DEFAULT 10 CHECK (total_rewinds_earned >= 0),
    rewinds_used INTEGER NOT NULL DEFAULT 0 CHECK (rewinds_used >= 0),
    total_spirit_stones BIGINT NOT NULL DEFAULT 0 CHECK (total_spirit_stones >= 0),
    successful_trials INTEGER NOT NULL DEFAULT 0 CHECK (successful_trials >= 0),
    failed_trials INTEGER NOT NULL DEFAULT 0 CHECK (failed_trials >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE roll_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    roll_type VARCHAR(128) NOT NULL CHECK (roll_type <> ''),
    success BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_roll_history_conversation ON roll_history(conversation_id, created_at DESC);
CREATE INDEX idx_roll_history_success_type ON roll_history(conversation_id, success, roll_type);

CREATE TABLE challenge_runs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL UNIQUE,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    result VARCHAR(16) NOT NULL CHECK (result IN ('good', 'bad', 'neutral')),
    reward_key VARCHAR(128),
    reward_amount BIGINT NOT NULL DEFAULT 0 CHECK (reward_amount >= 0),
    settlement_detail JSONB NOT NULL CHECK (jsonb_typeof(settlement_detail) = 'object'),
    settled_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_challenge_runs_conversation
        FOREIGN KEY (conversation_id, player_id, challenge_version_id)
        REFERENCES conversations(id, player_id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT challenge_runs_reward_shape CHECK (
        (result = 'good' AND reward_key IS NOT NULL)
        OR
        (result IN ('bad', 'neutral') AND reward_key IS NULL AND reward_amount = 0)
    )
);
CREATE INDEX idx_challenge_runs_player ON challenge_runs(player_id, settled_at DESC);
CREATE INDEX idx_challenge_runs_version ON challenge_runs(challenge_version_id);

COMMENT ON TABLE challenge_packs IS '稳定的挑战入口；运行时内容由 current_version_id 指向已发布快照';
COMMENT ON TABLE challenge_versions IS '不可原地修改的挑战版本；草稿发布时计算 content_hash';
COMMENT ON TABLE challenge_assets IS '挑战版本拥有的角色、Persona、场景、Prompt 与世界书 JSON';
COMMENT ON TABLE challenge_reward_rules IS '由后端验证和计算的版本化奖励规则';
COMMENT ON TABLE challenge_runs IS '每个挑战对话唯一的原子结算流水';
COMMENT ON COLUMN conversations.challenge_hash IS '创建对话时固定的已发布内容哈希';
COMMENT ON TABLE system_settings IS '管理员维护的平台全局判定设置';
COMMENT ON TABLE auth_settings IS '管理员维护的邮箱认证策略、SMTP 与验证码模板；SMTP 密码仅保存应用层密文';
COMMENT ON TABLE email_verification_codes IS '短期邮箱验证码，仅保存 bcrypt 摘要并限制尝试次数';
COMMENT ON TABLE invitation_codes IS '管理员创建的邀请码，仅保存高熵邀请码的 SHA-256 摘要';

COMMIT;
