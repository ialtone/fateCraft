-- +goose Up
-- Fatecraft PostgreSQL schema baseline.
-- This project started migration tracking at version 1; no older schema is supported.
-- Until the baseline is frozen, edits require validation in a fresh schema.
-- Goose tracks versions, not changes to an already-applied file.

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE TABLE users (
    id UUID PRIMARY KEY,
    username VARCHAR(32) NOT NULL,
    email VARCHAR(320) UNIQUE,
    password_hash TEXT,
    display_name VARCHAR(128) NOT NULL,
    avatar_url TEXT,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    role VARCHAR(16) NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin', 'owner')),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_users_status ON users(status);
-- 用户名大小写不敏感唯一，登录按 lower(username) 查询；后端按索引名识别用户名冲突，改名需同步 storage/auth.go。
CREATE UNIQUE INDEX idx_users_username_lower ON users (lower(username));
-- 必要的安全约束：初始化完成后 owner 只能有一个；这不是用于查询的普通角色索引。
CREATE UNIQUE INDEX idx_users_single_owner ON users ((role)) WHERE role = 'owner';

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
    purpose VARCHAR(16) NOT NULL DEFAULT 'login' CHECK (purpose IN ('login', 'bind')),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    browser_binding_hash CHAR(64) NOT NULL,
    return_to TEXT NOT NULL DEFAULT '/',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_oauth_login_flows_expires ON oauth_login_flows(expires_at);

CREATE TABLE auth_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    email_auth_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    linuxdo_login_enabled BOOLEAN NOT NULL DEFAULT TRUE,
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

CREATE TABLE challenge_packs (
    source VARCHAR(16) NOT NULL DEFAULT 'editor' CHECK (source IN ('builtin','upload','editor')),
    management_revision BIGINT NOT NULL DEFAULT 1 CHECK (management_revision > 0),
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
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(provenance)='object'),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    run_policy VARCHAR(16) NOT NULL DEFAULT 'allowed' CHECK (run_policy IN ('allowed','blocked')),
    run_policy_reason TEXT NOT NULL DEFAULT '',
    policy_revision BIGINT NOT NULL DEFAULT 1 CHECK (policy_revision > 0),
    policy_actor UUID REFERENCES users(id) ON DELETE SET NULL,
    policy_changed_at TIMESTAMPTZ,
    id UUID PRIMARY KEY,
    challenge_pack_id UUID NOT NULL REFERENCES challenge_packs(id) ON DELETE RESTRICT,
    version VARCHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
    manifest JSONB NOT NULL,
    content_hash VARCHAR(64) NOT NULL DEFAULT '',
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

CREATE TABLE challenge_runtime_configs (
    challenge_version_id UUID PRIMARY KEY REFERENCES challenge_versions(id) ON DELETE CASCADE,
    engine_key VARCHAR(128) NOT NULL,
    engine_version INTEGER NOT NULL CHECK (engine_version > 0),
    mode VARCHAR(16) NOT NULL DEFAULT 'challenge' CHECK (mode IN ('challenge', 'campaign')),
    campaign_config JSONB CHECK (campaign_config IS NULL OR jsonb_typeof(campaign_config) = 'object'),
    state_schema JSONB NOT NULL CHECK (jsonb_typeof(state_schema) = 'object'),
    action_config JSONB NOT NULL CHECK (jsonb_typeof(action_config) = 'object'),
    settlement_config JSONB NOT NULL CHECK (jsonb_typeof(settlement_config) = 'object'),
    presentation JSONB NOT NULL CHECK (jsonb_typeof(presentation) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT challenge_runtime_campaign_pair CHECK (
        (mode = 'challenge' AND campaign_config IS NULL)
        OR (mode = 'campaign' AND jsonb_typeof(campaign_config) = 'object')
    )
);

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

CREATE TABLE gameplay_definitions (
    id UUID PRIMARY KEY,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE CASCADE,
    -- Definition kinds and shapes are validated by the pinned trusted module.
    kind VARCHAR(128) NOT NULL CHECK (kind ~ '^[a-z0-9][a-z0-9._-]*$'),
    key VARCHAR(128) NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9._-]*$'),
    name VARCHAR(128) NOT NULL,
    content JSONB NOT NULL CHECK (jsonb_typeof(content) = 'object'),
    tags TEXT[] NOT NULL DEFAULT '{}',
    search_text TEXT NOT NULL DEFAULT '',
    visibility VARCHAR(16) NOT NULL DEFAULT 'player'
        CHECK (visibility IN ('player', 'model', 'internal')),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (challenge_version_id, kind, key),
    UNIQUE (id, challenge_version_id)
);
CREATE INDEX idx_gameplay_definitions_catalog
    ON gameplay_definitions(challenge_version_id, kind, sort_order, key);

CREATE TABLE challenge_outcome_rules (
    reward_points BIGINT NOT NULL DEFAULT 0 CHECK (reward_points BETWEEN 0 AND 9007199254740991),
    id UUID PRIMARY KEY,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE CASCADE,
    outcome_key VARCHAR(128) NOT NULL CHECK (outcome_key ~ '^[a-z0-9][a-z0-9._-]*$'),
    category VARCHAR(16) NOT NULL CHECK (category IN ('success', 'failure', 'neutral')),
    title VARCHAR(128) NOT NULL,
    tone VARCHAR(16) NOT NULL DEFAULT 'info' CHECK (tone IN ('success', 'error', 'warning', 'info')),
    verifier_key VARCHAR(128) NOT NULL,
    verifier_config JSONB NOT NULL CHECK (jsonb_typeof(verifier_config) = 'object'),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (challenge_version_id, outcome_key),
    UNIQUE (id, challenge_version_id)
);


CREATE TABLE campaigns (
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(256) NOT NULL,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    challenge_hash CHAR(64) NOT NULL,
    character_asset_id UUID,
    persona_asset_id UUID,
    scenario_asset_id UUID,
    status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'completed', 'dead', 'abandoned')),
    outcome_key VARCHAR(128) NOT NULL DEFAULT '',
    completed_at_game_time BIGINT CHECK (completed_at_game_time >= 0),
    current_location_key VARCHAR(128) NOT NULL,
    game_time BIGINT NOT NULL DEFAULT 0 CHECK (game_time >= 0),
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    event_sequence BIGINT NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    -- 权威状态的规范化哈希；服务创建和每次成功动作都会重算，默认值仅保护测试/直接插入。
    state_hash CHAR(64) NOT NULL DEFAULT repeat('0', 64)
        CHECK (state_hash ~ '^[0-9a-f]{64}$'),
    random_seed BIGINT NOT NULL CHECK (random_seed >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 创建时固化的 LLM 请求路由，之后不可修改（触发器兜底）；client 路由对象不产生任何账号收益。
    llm_route VARCHAR(16) NOT NULL CHECK (llm_route IN ('server', 'client')),
    UNIQUE (id, player_id, challenge_version_id, challenge_hash, llm_route),
    CONSTRAINT fk_campaigns_character_version
        FOREIGN KEY (character_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT fk_campaigns_persona_version
        FOREIGN KEY (persona_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT fk_campaigns_scenario_version
        FOREIGN KEY (scenario_asset_id, challenge_version_id)
        REFERENCES challenge_assets(id, challenge_version_id) ON DELETE RESTRICT
);
CREATE INDEX idx_campaigns_player_updated ON campaigns(player_id, updated_at DESC);
CREATE INDEX idx_campaigns_player_status ON campaigns(player_id, status);
CREATE INDEX idx_campaigns_challenge_version ON campaigns(challenge_version_id);

CREATE TABLE campaign_runtime_states (
    campaign_id UUID PRIMARY KEY REFERENCES campaigns(id) ON DELETE RESTRICT,
    state JSONB NOT NULL CHECK (jsonb_typeof(state) = 'object' AND octet_length(state::text) <= 73728)
);

CREATE TABLE campaign_characters (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL UNIQUE REFERENCES campaigns(id) ON DELETE RESTRICT,
    name VARCHAR(128) NOT NULL DEFAULT '无名修士',
    origin VARCHAR(256) NOT NULL DEFAULT '',
    public_identity VARCHAR(256) NOT NULL DEFAULT '',
    realm_key VARCHAR(128) NOT NULL,
    stage_key VARCHAR(128) NOT NULL,
    realm_rank INTEGER NOT NULL CHECK (realm_rank >= 0),
    cultivation_progress BIGINT NOT NULL DEFAULT 0 CHECK (cultivation_progress >= 0),
    cultivation_required BIGINT NOT NULL CHECK (cultivation_required > 0),
    foundation_quality INTEGER NOT NULL DEFAULT 50 CHECK (foundation_quality BETWEEN 0 AND 100),
    age_days BIGINT NOT NULL DEFAULT 5840 CHECK (age_days >= 0),
    lifespan_days BIGINT NOT NULL CHECK (lifespan_days > 0),
    health BIGINT NOT NULL CHECK (health >= 0),
    base_max_health BIGINT NOT NULL CHECK (base_max_health > 0),
    max_health BIGINT NOT NULL CHECK (max_health > 0 AND health <= max_health),
    mana BIGINT NOT NULL CHECK (mana >= 0),
    base_max_mana BIGINT NOT NULL CHECK (base_max_mana > 0),
    max_mana BIGINT NOT NULL CHECK (max_mana > 0 AND mana <= max_mana),
    spiritual_sense BIGINT NOT NULL CHECK (spiritual_sense >= 0),
    base_max_spiritual_sense BIGINT NOT NULL CHECK (base_max_spiritual_sense > 0),
    max_spiritual_sense BIGINT NOT NULL CHECK (max_spiritual_sense > 0 AND spiritual_sense <= max_spiritual_sense),
    injuries JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(injuries) = 'object'),
    long_term_effects JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(long_term_effects) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (id, campaign_id)
);

CREATE TABLE campaign_techniques (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    technique_key VARCHAR(128) NOT NULL,
    category VARCHAR(32) NOT NULL CHECK (category IN ('cultivation', 'body', 'movement', 'spell', 'secret', 'craft')),
    learned_layer INTEGER NOT NULL DEFAULT 1 CHECK (learned_layer > 0),
    mastery BIGINT NOT NULL DEFAULT 0 CHECK (mastery BETWEEN 0 AND 10000),
    insight BIGINT NOT NULL DEFAULT 0 CHECK (insight >= 0),
    equipped_slot VARCHAR(64),
    status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'sealed', 'damaged', 'abandoned')),
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_snapshot) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, technique_key)
);
CREATE UNIQUE INDEX uq_campaign_techniques_slot
    ON campaign_techniques(campaign_id, equipped_slot) WHERE equipped_slot IS NOT NULL;

CREATE TABLE campaign_item_stacks (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    item_key VARCHAR(128) NOT NULL,
    quality_key VARCHAR(128) NOT NULL DEFAULT 'common',
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, item_key, quality_key)
);

CREATE TABLE campaign_item_instances (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    definition_key VARCHAR(128) NOT NULL,
    quality_key VARCHAR(128) NOT NULL DEFAULT 'common',
    state VARCHAR(16) NOT NULL DEFAULT 'stored'
        CHECK (state IN ('stored', 'equipped', 'damaged', 'sealed', 'consumed')),
    equipped_slot VARCHAR(64),
    refinement INTEGER NOT NULL DEFAULT 0 CHECK (refinement BETWEEN 0 AND 10000),
    durability INTEGER NOT NULL CHECK (durability >= 0),
    max_durability INTEGER NOT NULL CHECK (max_durability > 0 AND durability <= max_durability),
    bound_character_id UUID,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(properties) = 'object'),
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(provenance) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT campaign_item_equipped_pair CHECK (
        (state = 'equipped' AND equipped_slot IS NOT NULL)
        OR (state <> 'equipped' AND equipped_slot IS NULL)
    ),
    CONSTRAINT fk_campaign_item_bound_character
        FOREIGN KEY (bound_character_id, campaign_id)
        REFERENCES campaign_characters(id, campaign_id) ON DELETE RESTRICT
);
CREATE INDEX idx_campaign_item_instances_campaign ON campaign_item_instances(campaign_id, state);
CREATE UNIQUE INDEX uq_campaign_item_instances_slot
    ON campaign_item_instances(campaign_id, equipped_slot) WHERE equipped_slot IS NOT NULL;

CREATE TABLE campaign_known_recipes (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    recipe_key VARCHAR(128) NOT NULL,
    learned_at_game_time BIGINT NOT NULL CHECK (learned_at_game_time >= 0),
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_snapshot) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, recipe_key)
);

CREATE TABLE campaign_market_states (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    market_key VARCHAR(128) NOT NULL,
    location_key VARCHAR(128) NOT NULL,
    last_restock_game_time BIGINT NOT NULL DEFAULT 0 CHECK (last_restock_game_time >= 0),
    price_modifier_bps INTEGER NOT NULL DEFAULT 10000 CHECK (price_modifier_bps BETWEEN 1000 AND 30000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, market_key)
);
CREATE INDEX idx_campaign_market_states_location
    ON campaign_market_states(campaign_id, location_key);

CREATE TABLE campaign_market_stocks (
    campaign_id UUID NOT NULL,
    market_key VARCHAR(128) NOT NULL,
    item_kind VARCHAR(16) NOT NULL CHECK (item_kind IN ('item', 'equipment')),
    item_key VARCHAR(128) NOT NULL,
    quality_key VARCHAR(128) NOT NULL DEFAULT 'common',
    quantity BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, market_key, item_kind, item_key, quality_key),
    CONSTRAINT fk_campaign_market_stock_state
        FOREIGN KEY (campaign_id, market_key)
        REFERENCES campaign_market_states(campaign_id, market_key) ON DELETE RESTRICT
);

CREATE TABLE campaign_location_discoveries (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    location_key VARCHAR(128) NOT NULL,
    discovered_at_game_time BIGINT NOT NULL CHECK (discovered_at_game_time >= 0),
    visited BOOLEAN NOT NULL DEFAULT FALSE,
    knowledge JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(knowledge) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, location_key)
);

CREATE TABLE campaign_npcs (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    archetype_key VARCHAR(128) NOT NULL,
    instance_key VARCHAR(128) NOT NULL,
    stable_name VARCHAR(128) NOT NULL,
    faction_key VARCHAR(128) NOT NULL DEFAULT '',
    realm_key VARCHAR(128) NOT NULL,
    stage_key VARCHAR(128) NOT NULL,
    current_location_key VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'missing', 'dead', 'departed', 'sealed')),
    public_profile JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(public_profile) = 'object'),
    private_state JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(private_state) = 'object'),
    goals JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(goals) = 'object'),
    last_active_at BIGINT NOT NULL DEFAULT 0 CHECK (last_active_at >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, instance_key),
    UNIQUE (id, campaign_id)
);
CREATE INDEX idx_campaign_npcs_location ON campaign_npcs(campaign_id, current_location_key, status);

CREATE TABLE campaign_relationships (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    npc_id UUID NOT NULL,
    trust INTEGER NOT NULL DEFAULT 0 CHECK (trust BETWEEN -100 AND 100),
    fear INTEGER NOT NULL DEFAULT 0 CHECK (fear BETWEEN 0 AND 100),
    affinity INTEGER NOT NULL DEFAULT 0 CHECK (affinity BETWEEN -100 AND 100),
    hostility INTEGER NOT NULL DEFAULT 0 CHECK (hostility BETWEEN 0 AND 100),
    debt BIGINT NOT NULL DEFAULT 0,
    public_stance VARCHAR(32) NOT NULL DEFAULT 'neutral',
    known_facts JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(known_facts) = 'object'),
    last_interaction_at BIGINT NOT NULL DEFAULT 0 CHECK (last_interaction_at >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, npc_id),
    CONSTRAINT fk_campaign_relationship_npc
        FOREIGN KEY (npc_id, campaign_id)
        REFERENCES campaign_npcs(id, campaign_id) ON DELETE RESTRICT
);

CREATE TABLE campaign_faction_standings (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    faction_key VARCHAR(128) NOT NULL,
    reputation INTEGER NOT NULL DEFAULT 0 CHECK (reputation BETWEEN -10000 AND 10000),
    position_key VARCHAR(128) NOT NULL DEFAULT 'outsider',
    contribution BIGINT NOT NULL DEFAULT 0 CHECK (contribution BETWEEN 0 AND 1000000000000),
    wanted_level INTEGER NOT NULL DEFAULT 0 CHECK (wanted_level BETWEEN 0 AND 5),
    permissions JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(permissions) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, faction_key)
);

CREATE TABLE campaign_scheduled_events (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    event_kind VARCHAR(32) NOT NULL CHECK (event_kind IN ('world_event', 'quest_deadline')),
    template_key VARCHAR(128) NOT NULL,
    due_game_time BIGINT NOT NULL CHECK (due_game_time >= 0),
    priority INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 1000),
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'cancelled')),
    occurrence BIGINT NOT NULL DEFAULT 0 CHECK (occurrence >= 0),
    scope JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(scope) = 'object'),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    processed_at_game_time BIGINT CHECK (processed_at_game_time >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, event_kind, template_key, occurrence)
);
CREATE INDEX idx_campaign_scheduled_events_due
    ON campaign_scheduled_events(campaign_id, due_game_time, priority DESC, event_kind, template_key, occurrence, id)
    WHERE status = 'pending';

CREATE TABLE campaign_region_states (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    region_key VARCHAR(128) NOT NULL,
    resource_level BIGINT NOT NULL CHECK (resource_level BETWEEN 0 AND 10000),
    danger_level BIGINT NOT NULL CHECK (danger_level BETWEEN 0 AND 10000),
    stability BIGINT NOT NULL CHECK (stability BETWEEN 0 AND 10000),
    conflict_stage BIGINT NOT NULL CHECK (conflict_stage BETWEEN 0 AND 100),
    faction_power JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(faction_power) = 'object'),
    last_event_key VARCHAR(128) NOT NULL DEFAULT '',
    last_event_variant VARCHAR(128) NOT NULL DEFAULT '',
    last_event_summary VARCHAR(256) NOT NULL DEFAULT '',
    last_event_game_time BIGINT CHECK (last_event_game_time >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, region_key)
);

CREATE TABLE campaign_quests (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    quest_key VARCHAR(128) NOT NULL,
    quest_type VARCHAR(16) NOT NULL CHECK (quest_type IN ('quest', 'obligation')),
    status VARCHAR(16) NOT NULL CHECK (status IN ('offered', 'active', 'blocked', 'completed', 'failed', 'expired')),
    offered_at_game_time BIGINT NOT NULL CHECK (offered_at_game_time >= 0),
    accepted_at_game_time BIGINT CHECK (accepted_at_game_time >= 0),
    due_game_time BIGINT CHECK (due_game_time >= 0),
    resolved_at_game_time BIGINT CHECK (resolved_at_game_time >= 0),
    progress JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(progress) = 'object'),
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_snapshot) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, quest_key),
    CONSTRAINT campaign_quest_acceptance_pair CHECK (
        (status = 'offered' AND accepted_at_game_time IS NULL)
        OR (status <> 'offered' AND accepted_at_game_time IS NOT NULL)
    ),
    CONSTRAINT campaign_quest_resolution_pair CHECK (
        (status IN ('completed', 'failed', 'expired') AND resolved_at_game_time IS NOT NULL)
        OR (status NOT IN ('completed', 'failed', 'expired') AND resolved_at_game_time IS NULL)
    )
);
CREATE INDEX idx_campaign_quests_status ON campaign_quests(campaign_id, quest_type, status, due_game_time);

CREATE TABLE campaign_encounters (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    encounter_key VARCHAR(128) NOT NULL,
    location_key VARCHAR(128) NOT NULL,
    opponent_key VARCHAR(128) NOT NULL,
    opponent_name VARCHAR(128) NOT NULL,
    opponent_realm_rank INTEGER NOT NULL CHECK (opponent_realm_rank >= 0),
    opponent_health BIGINT NOT NULL CHECK (opponent_health >= 0),
    opponent_max_health BIGINT NOT NULL CHECK (opponent_max_health > 0 AND opponent_health <= opponent_max_health),
    opponent_attack BIGINT NOT NULL CHECK (opponent_attack >= 0),
    opponent_defense BIGINT NOT NULL CHECK (opponent_defense >= 0),
    player_damage_base BIGINT NOT NULL CHECK (player_damage_base > 0),
    player_damage_scale_bps BIGINT NOT NULL CHECK (player_damage_scale_bps BETWEEN 0 AND 100000),
    round INTEGER NOT NULL DEFAULT 1 CHECK (round >= 1),
    max_rounds INTEGER NOT NULL CHECK (max_rounds >= 1 AND round <= max_rounds),
    status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'won', 'lost', 'fled', 'abandoned')),
    last_move VARCHAR(32) NOT NULL DEFAULT '',
    started_at_game_time BIGINT NOT NULL CHECK (started_at_game_time >= 0),
    resolved_at_game_time BIGINT CHECK (resolved_at_game_time >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, id)
);
CREATE INDEX idx_campaign_encounters_active ON campaign_encounters(campaign_id, status, created_at DESC);
CREATE INDEX idx_campaign_encounters_quest ON campaign_encounters(campaign_id, encounter_key, status);

CREATE TABLE conversations (
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(256) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ended', 'archived', 'deleted')),
    is_processing BOOLEAN NOT NULL DEFAULT FALSE,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    challenge_hash CHAR(64) NOT NULL,
    campaign_id UUID,
    character_asset_id UUID,
    persona_asset_id UUID,
    scenario_asset_id UUID,
    branch_epoch BIGINT NOT NULL DEFAULT 0 CHECK (branch_epoch >= 0),
    next_turn_seq BIGINT NOT NULL DEFAULT 1 CHECK (next_turn_seq > 0),
    next_message_seq BIGINT NOT NULL DEFAULT 1 CHECK (next_message_seq > 0),
    current_memory_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    -- 创建时固化的 LLM 请求路由，不可修改（触发器兜底）；必须等于所属 Campaign 路由（复合外键兜底）。
    llm_route VARCHAR(16) NOT NULL CHECK (llm_route IN ('server', 'client')),
    UNIQUE (id, player_id, challenge_version_id),
    CONSTRAINT fk_conversations_campaign
        FOREIGN KEY (campaign_id, player_id, challenge_version_id, challenge_hash, llm_route)
        REFERENCES campaigns(id, player_id, challenge_version_id, challenge_hash, llm_route) ON DELETE RESTRICT,
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
CREATE INDEX idx_conversations_status ON conversations(status);
CREATE INDEX idx_conversations_player_updated ON conversations(player_id, updated_at DESC);
CREATE INDEX idx_conversations_challenge_version ON conversations(challenge_version_id);
CREATE INDEX idx_conversations_title_trgm ON conversations USING GIN (title public.gin_trgm_ops);
-- 已删除会话保留用于恢复，但不再占用 Campaign 的唯一活动会话槽位。
CREATE UNIQUE INDEX idx_conversations_campaign_one_active
    ON conversations(campaign_id)
    WHERE campaign_id IS NOT NULL AND status <> 'deleted';

-- 每次可写生成都有独立的 durable owner。活动状态的部分唯一索引
-- 与 conversation 行锁共同保证同一对话最多一个生成任务。
CREATE TABLE conversation_generation_runs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	request_id UUID NOT NULL UNIQUE,
	operation_type VARCHAR(32) NOT NULL CHECK (operation_type IN ('user_input', 'regenerate', 'continue', 'rewrite')),
	target_message_id UUID,
	payload_hash VARCHAR(64) NOT NULL,
    conversation_revision BIGINT NOT NULL CHECK (conversation_revision >= 0),
    status VARCHAR(24) NOT NULL CHECK (status IN (
        'running', 'cancel_requested', 'completed', 'failed', 'cancelled', 'abandoned'
    )),
    owner_instance_id VARCHAR(128) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    error_code VARCHAR(128) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX uq_conversation_generation_active
    ON conversation_generation_runs(conversation_id)
    WHERE status IN ('running', 'cancel_requested');
CREATE INDEX idx_conversation_generation_recovery
    ON conversation_generation_runs(status, lease_expires_at)
    WHERE status IN ('running', 'cancel_requested');
CREATE INDEX idx_conversation_generation_history
    ON conversation_generation_runs(conversation_id, started_at DESC);

CREATE TABLE conversation_turns (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    turn_seq BIGINT NOT NULL CHECK (turn_seq > 0),
    trigger_type VARCHAR(32) NOT NULL CHECK (trigger_type IN (
        'opening', 'user_input', 'continue', 'rewrite', 'regenerate', 'admin_mutation'
    )),
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending', 'completed', 'partial', 'failed', 'cancelled')),
    failure_stage VARCHAR(64) NOT NULL DEFAULT '',
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sealed_at TIMESTAMPTZ,
    UNIQUE (conversation_id, turn_seq),
    UNIQUE (conversation_id, id)
);
CREATE UNIQUE INDEX uq_conversation_turn_pending
    ON conversation_turns(conversation_id)
    WHERE status = 'pending' AND invalidated_at IS NULL;
CREATE INDEX idx_conversation_turns_completed
    ON conversation_turns(conversation_id, turn_seq)
    WHERE status = 'completed' AND invalidated_at IS NULL;

CREATE TABLE campaign_checkpoints (
    turn_id UUID PRIMARY KEY REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object' AND octet_length(snapshot::text) <= 4718592)
);

CREATE TABLE generation_attempts (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    turn_id UUID NOT NULL,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    branch_epoch BIGINT NOT NULL CHECK (branch_epoch >= 0),
    memory_id UUID,
    memory_covered_until_turn_seq BIGINT NOT NULL DEFAULT 0 CHECK (memory_covered_until_turn_seq >= 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('running', 'completed', 'partial', 'failed', 'cancelled')),
    step_count INTEGER NOT NULL DEFAULT 0 CHECK (step_count >= 0),
    committed_mutating_tool_count INTEGER NOT NULL DEFAULT 0 CHECK (committed_mutating_tool_count >= 0),
    tool_catalog_version VARCHAR(128) NOT NULL DEFAULT '',
    runtime_hash VARCHAR(128) NOT NULL DEFAULT '',
    failure_stage VARCHAR(64) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,
    UNIQUE (turn_id, attempt_no),
    UNIQUE (conversation_id, id),
    CONSTRAINT fk_generation_attempt_turn
        FOREIGN KEY (conversation_id, turn_id)
        REFERENCES conversation_turns(conversation_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_generation_attempt_running
    ON generation_attempts(turn_id)
    WHERE status = 'running';

CREATE TABLE agent_steps (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    turn_id UUID NOT NULL,
    attempt_id UUID NOT NULL,
    step_no INTEGER NOT NULL CHECK (step_no > 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    output_lane VARCHAR(16) NOT NULL DEFAULT 'undecided' CHECK (output_lane IN ('undecided', 'text', 'tool')),
    model VARCHAR(128) NOT NULL,
    protocol VARCHAR(32) NOT NULL CHECK (protocol IN ('chat_completions', 'responses')),
    tool_snapshot_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tool_snapshot_json) = 'array'),
    tool_snapshot_hash CHAR(64) NOT NULL,
    campaign_revision BIGINT CHECK (campaign_revision >= 0),
    finish_reason VARCHAR(64) NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    token_usage_source VARCHAR(16) NOT NULL DEFAULT 'estimated' CHECK (token_usage_source IN ('provider', 'estimated')),
    first_event_ms BIGINT NOT NULL DEFAULT -1 CHECK (first_event_ms >= -1),
    first_text_ms BIGINT NOT NULL DEFAULT -1 CHECK (first_text_ms >= -1),
    model_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (model_duration_ms >= 0),
    generation_ms BIGINT NOT NULL DEFAULT 0 CHECK (generation_ms >= 0),
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,
    UNIQUE (attempt_id, step_no),
    UNIQUE (id, attempt_id),
    CONSTRAINT fk_agent_step_turn
        FOREIGN KEY (conversation_id, turn_id)
        REFERENCES conversation_turns(conversation_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_step_attempt
        FOREIGN KEY (conversation_id, attempt_id)
        REFERENCES generation_attempts(conversation_id, id) ON DELETE CASCADE
);

CREATE TABLE agent_tool_invocations (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    turn_id UUID NOT NULL,
    attempt_id UUID NOT NULL,
    step_id UUID NOT NULL,
    step_tool_snapshot_hash CHAR(64) NOT NULL,
    call_id VARCHAR(256) NOT NULL,
    tool_name VARCHAR(128) NOT NULL CHECK (tool_name ~ '^[a-z][a-z0-9_]{0,127}$'),
    arguments_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(arguments_json) = 'object'),
    arguments_hash CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('received', 'rejected', 'committed', 'failed', 'abandoned')),
    result_code VARCHAR(64) NOT NULL DEFAULT '',
    result_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(result_json) = 'object'),
    is_mutating BOOLEAN NOT NULL DEFAULT FALSE,
    committed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (attempt_id, call_id),
    UNIQUE (id, attempt_id),
    CONSTRAINT fk_agent_tool_turn FOREIGN KEY (conversation_id, turn_id)
        REFERENCES conversation_turns(conversation_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_tool_attempt FOREIGN KEY (conversation_id, attempt_id)
        REFERENCES generation_attempts(conversation_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_tool_step FOREIGN KEY (step_id, attempt_id)
        REFERENCES agent_steps(id, attempt_id) ON DELETE CASCADE
);
CREATE INDEX idx_agent_tool_invocations_attempt ON agent_tool_invocations(attempt_id, created_at);

CREATE TABLE chat_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool', 'internal')),
    kind VARCHAR(16) NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'tool_call', 'tool_result')),
    content TEXT NOT NULL,
    content_blocks JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(content_blocks) = 'array'),
    tool_call_id VARCHAR(256) NOT NULL DEFAULT '',
    tool_name VARCHAR(128) NOT NULL DEFAULT '',
    step_id UUID REFERENCES agent_steps(id) ON DELETE RESTRICT,
    tool_invocation_id UUID REFERENCES agent_tool_invocations(id) ON DELETE RESTRICT,
    hidden BOOLEAN NOT NULL DEFAULT FALSE,
    token_count INTEGER,
    turn_id UUID,
    attempt_id UUID,
    message_seq BIGINT NOT NULL CHECK (message_seq > 0),
    tokenizer_version VARCHAR(32) NOT NULL DEFAULT 'heuristic-v1',
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    parent_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (conversation_id, message_seq),
    CONSTRAINT fk_chat_message_turn
        FOREIGN KEY (conversation_id, turn_id)
        REFERENCES conversation_turns(conversation_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_chat_message_attempt
        FOREIGN KEY (conversation_id, attempt_id)
        REFERENCES generation_attempts(conversation_id, id) ON DELETE RESTRICT,
    CONSTRAINT ck_chat_message_tool_shape CHECK (
        (kind = 'text' AND tool_invocation_id IS NULL) OR
        (kind = 'tool_call' AND role = 'assistant' AND step_id IS NOT NULL AND tool_invocation_id IS NOT NULL AND tool_call_id <> '' AND tool_name <> '' AND jsonb_array_length(content_blocks) >= 1) OR
        (kind = 'tool_result' AND role = 'tool' AND step_id IS NOT NULL AND tool_invocation_id IS NOT NULL AND tool_call_id <> '' AND tool_name <> '')
    ),
    UNIQUE (tool_invocation_id, kind)
);
CREATE INDEX idx_chat_messages_active ON chat_messages(conversation_id, message_seq) WHERE invalidated_at IS NULL;
CREATE INDEX idx_chat_messages_active_turn_role ON chat_messages(conversation_id, turn_id, role)
    WHERE invalidated_at IS NULL;

CREATE TABLE campaign_events (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    event_type VARCHAR(128) NOT NULL CHECK (event_type ~ '^[a-z0-9][a-z0-9._-]*$'),
    visibility VARCHAR(16) NOT NULL CHECK (visibility IN ('player', 'model', 'internal')),
    actor_type VARCHAR(16) NOT NULL CHECK (actor_type IN ('player', 'model', 'system')),
    actor_id VARCHAR(160),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    source_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    turn_id UUID,
    attempt_id UUID,
    idempotency_key VARCHAR(160) NOT NULL,
    game_time BIGINT NOT NULL CHECK (game_time >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, sequence),
    CONSTRAINT fk_campaign_event_turn FOREIGN KEY (turn_id) REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_campaign_event_attempt FOREIGN KEY (attempt_id) REFERENCES generation_attempts(id) ON DELETE RESTRICT
);

CREATE TABLE campaign_action_commits (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(160) NOT NULL,
    action_key VARCHAR(128) NOT NULL CHECK (action_key ~ '^[a-z0-9][a-z0-9._-]*$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    first_sequence BIGINT NOT NULL CHECK (first_sequence > 0),
    last_sequence BIGINT NOT NULL CHECK (last_sequence >= first_sequence),
    result JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(result) = 'object'),
    -- 动作结果的稳定契约，供模型上下文、事实校验和结局汇总复用。
    narrative JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(narrative) = 'object' AND octet_length(narrative::text) <= 65536),
    state_hash CHAR(64) NOT NULL DEFAULT repeat('0', 64)
        CHECK (state_hash ~ '^[0-9a-f]{64}$'),
    schema_version SMALLINT NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    turn_id UUID,
    attempt_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campaign_id, idempotency_key),
    CONSTRAINT fk_campaign_action_turn FOREIGN KEY (turn_id) REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_campaign_action_attempt FOREIGN KEY (attempt_id) REFERENCES generation_attempts(id) ON DELETE RESTRICT,
    CONSTRAINT fk_campaign_action_first_event
        FOREIGN KEY (campaign_id, first_sequence)
        REFERENCES campaign_events(campaign_id, sequence) ON DELETE RESTRICT,
    CONSTRAINT fk_campaign_action_last_event
        FOREIGN KEY (campaign_id, last_sequence)
        REFERENCES campaign_events(campaign_id, sequence) ON DELETE RESTRICT
);
CREATE INDEX idx_campaign_action_commits_revision ON campaign_action_commits(campaign_id, revision);

CREATE TABLE display_messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    chat_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    type VARCHAR(16) NOT NULL,
    markdown TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    turn_id UUID,
    attempt_id UUID,
    -- Campaign 叙事与生成时权威快照的绑定；非 Campaign 消息保持 NULL。
    campaign_revision BIGINT CHECK (campaign_revision >= 0),
    campaign_state_hash CHAR(64) CHECK (campaign_state_hash ~ '^[0-9a-f]{64}$'),
    validation_status VARCHAR(16) NOT NULL DEFAULT 'not_applicable'
        CHECK (validation_status IN (
            'not_applicable', 'passed', 'repaired', 'fallback', 'stale', 'failed'
        )),
    validation_detail JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(validation_detail) = 'object'),
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_display_message_turn FOREIGN KEY (turn_id) REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_display_message_attempt FOREIGN KEY (attempt_id) REFERENCES generation_attempts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_display_messages_conversation ON display_messages(conversation_id, created_at);
CREATE INDEX idx_display_messages_active_cursor
    ON display_messages(conversation_id, created_at DESC, id DESC)
    WHERE invalidated_at IS NULL;
CREATE INDEX idx_display_messages_campaign_binding
    ON display_messages(conversation_id, campaign_revision, campaign_state_hash);

-- 一条叙事可能由同一回合的多个工具动作产生，因此使用独立关联表而不是单列。
CREATE TABLE display_message_campaign_actions (
    display_message_id UUID NOT NULL REFERENCES display_messages(id) ON DELETE CASCADE,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    action_id VARCHAR(160) NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    PRIMARY KEY (display_message_id, action_id),
    CONSTRAINT fk_display_message_campaign_action
        FOREIGN KEY (campaign_id, action_id)
        REFERENCES campaign_action_commits(campaign_id, idempotency_key)
        ON DELETE RESTRICT
);
CREATE INDEX idx_display_message_campaign_actions_action
    ON display_message_campaign_actions(campaign_id, action_id);

CREATE TABLE conversation_shares (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id UUID NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    branch_epoch BIGINT NOT NULL CHECK (branch_epoch >= 0),
    scope VARCHAR(16) NOT NULL CHECK (scope IN ('all', 'range')),
    source_start_message_id UUID NOT NULL REFERENCES display_messages(id) ON DELETE CASCADE,
    source_end_message_id UUID NOT NULL REFERENCES display_messages(id) ON DELETE CASCADE,
    snapshot_version INTEGER NOT NULL DEFAULT 1 CHECK (snapshot_version > 0),
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    snapshot_hash CHAR(64) NOT NULL CHECK (snapshot_hash ~ '^[0-9a-f]{64}$'),
    title VARCHAR(120) NOT NULL,
    public_attribution VARCHAR(48) NOT NULL DEFAULT '',
    message_count INTEGER NOT NULL CHECK (message_count BETWEEN 1 AND 500),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'moderated')),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ,
    moderated_at TIMESTAMPTZ,
    moderation_reason VARCHAR(64) NOT NULL DEFAULT '',
    UNIQUE (player_id, request_id),
    UNIQUE (id, player_id, conversation_id)
);
CREATE INDEX idx_conversation_shares_owner ON conversation_shares(player_id, conversation_id, created_at DESC, id DESC);
CREATE INDEX idx_conversation_shares_active ON conversation_shares(status, expires_at) WHERE status = 'active';

CREATE TABLE conversation_share_reports (
    id UUID PRIMARY KEY,
    share_id UUID NOT NULL REFERENCES conversation_shares(id) ON DELETE CASCADE,
    reporter_fingerprint CHAR(64) NOT NULL CHECK (reporter_fingerprint ~ '^[0-9a-f]{64}$'),
    reason VARCHAR(32) NOT NULL CHECK (reason IN ('privacy', 'abuse', 'copyright', 'other')),
    details VARCHAR(500) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (share_id, reporter_fingerprint)
);
CREATE INDEX idx_conversation_share_reports_status ON conversation_share_reports(status, created_at DESC);

CREATE TABLE conversation_share_audit_events (
    id UUID PRIMARY KEY,
    share_id UUID REFERENCES conversation_shares(id) ON DELETE SET NULL,
    report_id UUID REFERENCES conversation_share_reports(id) ON DELETE SET NULL,
    admin_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type VARCHAR(32) NOT NULL CHECK (event_type IN ('share_moderated', 'report_resolved', 'report_dismissed')),
    detail JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(detail) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_conversation_share_audit_events_created ON conversation_share_audit_events(created_at DESC, id DESC);
CREATE INDEX idx_conversation_share_audit_events_share ON conversation_share_audit_events(share_id, created_at DESC);

CREATE TABLE conversation_states (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    state JSONB NOT NULL DEFAULT '{}'::jsonb,
    schema_version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE state_revisions (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    source VARCHAR(16) NOT NULL CHECK (source IN ('ai', 'system', 'module')),
    related_message_id UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    patch JSONB NOT NULL,
    before_state JSONB NOT NULL,
    after_state JSONB NOT NULL,
    turn_id UUID,
    attempt_id UUID,
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_state_revision_turn FOREIGN KEY (turn_id) REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_state_revision_attempt FOREIGN KEY (attempt_id) REFERENCES generation_attempts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_state_revisions_cursor
    ON state_revisions(conversation_id, created_at DESC, id DESC)
    WHERE invalidated_at IS NULL;

CREATE TABLE module_action_commits (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    branch_epoch BIGINT NOT NULL CHECK (branch_epoch >= 0),
    request_id UUID NOT NULL,
    action_key VARCHAR(128) NOT NULL,
    arguments_hash VARCHAR(64) NOT NULL,
    turn_id UUID NOT NULL REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    result JSONB NOT NULL CHECK (jsonb_typeof(result) = 'object'),
    narrative JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(narrative) = 'object' AND octet_length(narrative::text) <= 65536),
    schema_version SMALLINT NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (conversation_id, branch_epoch, request_id)
);

CREATE TABLE conversation_memories (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    previous_memory_id UUID REFERENCES conversation_memories(id) ON DELETE RESTRICT,
    source_branch_epoch BIGINT NOT NULL CHECK (source_branch_epoch >= 0),
    covered_from_turn_seq BIGINT NOT NULL CHECK (covered_from_turn_seq > 0),
    covered_until_turn_seq BIGINT NOT NULL CHECK (covered_until_turn_seq >= covered_from_turn_seq),
    covered_until_message_seq BIGINT NOT NULL CHECK (covered_until_message_seq > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    prompt_version VARCHAR(64) NOT NULL,
    renderer_version VARCHAR(64) NOT NULL,
    content JSONB NOT NULL CHECK (jsonb_typeof(content) = 'object'),
    rendered_text TEXT NOT NULL,
    source_hash CHAR(64) NOT NULL,
    generation_fingerprint CHAR(64) NOT NULL,
    model VARCHAR(128) NOT NULL,
    token_count INTEGER NOT NULL CHECK (token_count >= 0),
    tokenizer_version VARCHAR(32) NOT NULL,
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (conversation_id, version),
    UNIQUE (conversation_id, id)
);
CREATE UNIQUE INDEX uq_conversation_memories_source
    ON conversation_memories(conversation_id, source_hash)
    WHERE invalidated_at IS NULL;

ALTER TABLE conversations
    ADD CONSTRAINT fk_conversations_current_memory
    FOREIGN KEY (id, current_memory_id)
    REFERENCES conversation_memories(conversation_id, id) ON DELETE RESTRICT;
ALTER TABLE generation_attempts
    ADD CONSTRAINT fk_generation_attempt_memory
    FOREIGN KEY (conversation_id, memory_id)
    REFERENCES conversation_memories(conversation_id, id) ON DELETE RESTRICT;

CREATE TABLE memory_jobs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    previous_memory_id UUID REFERENCES conversation_memories(id) ON DELETE RESTRICT,
    source_branch_epoch BIGINT NOT NULL CHECK (source_branch_epoch >= 0),
    target_until_turn_seq BIGINT NOT NULL CHECK (target_until_turn_seq > 0),
    source_hash CHAR(64),
    generation_fingerprint CHAR(64),
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    execution_count INTEGER NOT NULL DEFAULT 0 CHECK (execution_count >= 0),
    max_executions INTEGER NOT NULL DEFAULT 4 CHECK (max_executions > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    last_error_code VARCHAR(64) NOT NULL DEFAULT '',
    last_error_message VARCHAR(1024) NOT NULL DEFAULT '',
    result_memory_id UUID REFERENCES conversation_memories(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX uq_memory_jobs_active ON memory_jobs(conversation_id)
    WHERE status IN ('pending', 'running');
CREATE INDEX idx_memory_jobs_available ON memory_jobs(status, available_at, created_at);

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
    turn_id UUID REFERENCES conversation_turns(id) ON DELETE SET NULL,
    attempt_id UUID REFERENCES generation_attempts(id) ON DELETE SET NULL,
    branch_epoch BIGINT NOT NULL DEFAULT 0,
    memory_id UUID REFERENCES conversation_memories(id) ON DELETE SET NULL,
    memory_covered_until_turn_seq BIGINT NOT NULL DEFAULT 0,
    memory_lag BOOLEAN NOT NULL DEFAULT FALSE,
    omitted_turn_from BIGINT,
    omitted_turn_until BIGINT,
    memory_job_id UUID REFERENCES memory_jobs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_context_build_logs_conversation ON context_build_logs(conversation_id, created_at DESC, id DESC);
CREATE INDEX idx_context_build_logs_created ON context_build_logs(created_at DESC, id DESC);
CREATE INDEX idx_context_build_logs_attempt ON context_build_logs(attempt_id, created_at DESC, id DESC) WHERE attempt_id IS NOT NULL;

CREATE TABLE llm_settings (
    player_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    base_url VARCHAR(512) NOT NULL DEFAULT 'https://api.openai.com/v1',
    protocol VARCHAR(32) NOT NULL DEFAULT 'responses' CHECK (protocol IN ('chat_completions', 'responses')),
    api_key_encrypted TEXT,
    model VARCHAR(128) NOT NULL DEFAULT '',
    memory_model VARCHAR(128) NOT NULL DEFAULT '',
    inquiry_model VARCHAR(128) NOT NULL DEFAULT '',
    max_context_tokens INTEGER NOT NULL DEFAULT 128000,
    max_response_tokens INTEGER NOT NULL DEFAULT 16384,
    temperature DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    top_p DOUBLE PRECISION NOT NULL DEFAULT 0.95,
    stream_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
    memory_reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
    inquiry_reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
	settings_revision BIGINT NOT NULL DEFAULT 1 CHECK (settings_revision > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 用户自行配置 LLM 时的请求路由；client 路由的 Key 只存浏览器，服务端绝不保存。
    llm_route VARCHAR(16) NOT NULL DEFAULT 'server' CHECK (llm_route IN ('server', 'client')),
    CONSTRAINT chk_llm_settings_client_no_key CHECK (llm_route <> 'client' OR COALESCE(api_key_encrypted, '') = '')
);

CREATE TABLE system_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    installation_completed_at TIMESTAMPTZ,
    roll_system_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    exchange_tickets_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    roll_difficulty_mode VARCHAR(10) NOT NULL DEFAULT 'normal' CHECK (roll_difficulty_mode IN ('easy', 'normal', 'hard')),
    detailed_llm_logging_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    detailed_llm_logging_scope VARCHAR(16) NOT NULL DEFAULT 'all' CHECK (detailed_llm_logging_scope IN ('all', 'user', 'conversation')),
    detailed_llm_logging_target_id UUID,
    CONSTRAINT chk_detailed_llm_logging_target CHECK (
        (detailed_llm_logging_scope = 'all' AND detailed_llm_logging_target_id IS NULL)
        OR (detailed_llm_logging_scope IN ('user', 'conversation') AND detailed_llm_logging_target_id IS NOT NULL
            AND detailed_llm_logging_target_id <> '00000000-0000-0000-0000-000000000000'::uuid)
    ),
    diagnostic_log_cleanup_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    diagnostic_log_retention_days INTEGER NOT NULL DEFAULT 30 CHECK (diagnostic_log_retention_days BETWEEN 1 AND 3650),
    llm_access_mode VARCHAR(32) NOT NULL DEFAULT 'user_managed' CHECK (llm_access_mode IN ('user_managed', 'platform_managed')),
    -- 是否允许用户选择客户端直连（与 llm_access_mode 相互独立）；默认开启，由 owner 管理。
    client_llm_route_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    public_llm_base_url VARCHAR(512) NOT NULL DEFAULT 'https://api.openai.com/v1',
    public_llm_protocol VARCHAR(32) NOT NULL DEFAULT 'responses' CHECK (public_llm_protocol IN ('chat_completions', 'responses')),
    public_llm_api_key_encrypted TEXT,
    public_llm_model VARCHAR(128) NOT NULL DEFAULT '',
    public_llm_memory_model VARCHAR(128) NOT NULL DEFAULT '',
    public_llm_inquiry_model VARCHAR(128) NOT NULL DEFAULT '',
    public_llm_max_context_tokens INTEGER NOT NULL DEFAULT 128000,
    public_llm_max_response_tokens INTEGER NOT NULL DEFAULT 16384,
    public_llm_temperature DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    public_llm_top_p DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    public_llm_stream_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    public_llm_reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
    public_llm_memory_reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
    public_llm_inquiry_reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
    public_llm_settings_revision BIGINT NOT NULL DEFAULT 1 CHECK (public_llm_settings_revision > 0),
    -- LLM 服务端出站代理：仅影响后端模型请求，不影响浏览器、数据库、SMTP 等其他流量。
    llm_proxy_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    llm_proxy_url VARCHAR(2048) NOT NULL DEFAULT '',
    llm_proxy_username VARCHAR(256) NOT NULL DEFAULT '',
    llm_proxy_password_encrypted TEXT NOT NULL DEFAULT '',
    llm_proxy_ca_pem TEXT NOT NULL DEFAULT '',
    llm_proxy_revision BIGINT NOT NULL DEFAULT 0 CHECK (llm_proxy_revision >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO system_settings (id) VALUES (1);

CREATE TABLE admin_audit_logs (
	trace_id UUID,
    id UUID PRIMARY KEY,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    target_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(64) NOT NULL,
    old_role VARCHAR(16),
    new_role VARCHAR(16),
    result VARCHAR(16) NOT NULL CHECK (result IN ('success', 'rejected', 'failed')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    request_id VARCHAR(128),
    ip_hash CHAR(64),
    user_agent_hash CHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_admin_audit_created ON admin_audit_logs(created_at DESC,id DESC);
CREATE INDEX idx_admin_audit_actor ON admin_audit_logs(actor_user_id,created_at DESC,id DESC);

CREATE TABLE llm_request_logs (
	trace_id UUID,
	protocol TEXT NOT NULL DEFAULT 'responses',
	started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	transport_attempts INTEGER NOT NULL DEFAULT 0 CHECK (transport_attempts >= 0),
	input_tokens INTEGER CHECK (input_tokens >= 0),
	output_tokens INTEGER CHECK (output_tokens >= 0),
	error_code TEXT NOT NULL DEFAULT '',
	body_status TEXT NOT NULL DEFAULT 'disabled' CHECK (body_status IN ('disabled','captured','truncated','invalid')),
	body_metadata JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(body_metadata) = 'object'),
    id UUID PRIMARY KEY,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    message_id UUID,
    purpose VARCHAR(32) NOT NULL DEFAULT 'narrative' CHECK (purpose IN ('narrative', 'memory', 'character_inquiry', 'protocol_test')),
    attempt_id UUID REFERENCES generation_attempts(id) ON DELETE SET NULL,
    memory_job_id UUID REFERENCES memory_jobs(id) ON DELETE SET NULL,
    memory_id UUID REFERENCES conversation_memories(id) ON DELETE SET NULL,
    inquiry_id UUID,
    inquiry_snapshot_id UUID,
    inquiry_snapshot_branch_epoch BIGINT NOT NULL DEFAULT 0 CHECK (inquiry_snapshot_branch_epoch >= 0),
    inquiry_snapshot_revision BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(128) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    stream BOOLEAN NOT NULL,
    request_body JSONB NOT NULL CHECK (jsonb_typeof(request_body) = 'object'),
    raw_response TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    response_status INTEGER,
    finish_reason VARCHAR(64),
    received_done BOOLEAN NOT NULL DEFAULT FALSE,
    success BOOLEAN NOT NULL DEFAULT FALSE,
    audit_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT,
    duration_ms BIGINT NOT NULL CHECK (duration_ms >= 0),
	settings_revision BIGINT NOT NULL DEFAULT 0,
	settings_source VARCHAR(16) NOT NULL DEFAULT 'user' CHECK (settings_source IN ('user', 'platform', 'client')),
	output_mode VARCHAR(32) NOT NULL DEFAULT '',
	contract_name VARCHAR(64) NOT NULL DEFAULT '',
	contract_version VARCHAR(32) NOT NULL DEFAULT '',
	contract_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
	transport_success BOOLEAN NOT NULL DEFAULT FALSE,
	structured_success BOOLEAN NOT NULL DEFAULT FALSE,
	structured_error_code VARCHAR(128) NOT NULL DEFAULT '',
	output_types TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_llm_request_logs_created ON llm_request_logs(created_at DESC, id DESC);
CREATE INDEX idx_llm_request_logs_trace ON llm_request_logs(trace_id, created_at DESC, id DESC) WHERE trace_id IS NOT NULL;
CREATE INDEX idx_llm_request_logs_attempt ON llm_request_logs(attempt_id, created_at DESC, id DESC) WHERE attempt_id IS NOT NULL;
CREATE INDEX idx_llm_request_logs_conversation ON llm_request_logs(conversation_id, created_at DESC, id DESC);

CREATE TABLE character_inquiry_threads (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (conversation_id, player_id),
    UNIQUE (id, conversation_id, player_id)
);

CREATE TABLE character_inquiry_messages (
    id UUID PRIMARY KEY,
    thread_id UUID NOT NULL,
    conversation_id UUID NOT NULL,
    player_id UUID NOT NULL,
    role VARCHAR(16) NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    snapshot_id UUID,
    snapshot_branch_epoch BIGINT NOT NULL DEFAULT 0 CHECK (snapshot_branch_epoch >= 0),
    snapshot_revision BIGINT NOT NULL DEFAULT 0,
    source_refs JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(source_refs) = 'array'),
    status VARCHAR(16) NOT NULL CHECK (status IN ('completed', 'failed', 'cancelled')),
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_character_inquiry_thread
        FOREIGN KEY (thread_id, conversation_id, player_id)
        REFERENCES character_inquiry_threads(id, conversation_id, player_id) ON DELETE CASCADE
);
CREATE INDEX idx_character_inquiry_messages_thread
    ON character_inquiry_messages(thread_id, created_at DESC, id DESC);

CREATE TABLE character_inquiry_rate_limits (
    player_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    window_started_at TIMESTAMPTZ NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE player_reward_balances (
    refunded_total BIGINT NOT NULL DEFAULT 0 CHECK (refunded_total >= 0),
    reversed_total BIGINT NOT NULL DEFAULT 0 CHECK (reversed_total >= 0),
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reward_key VARCHAR(64) NOT NULL CHECK (reward_key IN ('fatecraft.points','fatecraft.message-rewrite-credit')),
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    granted_total BIGINT NOT NULL DEFAULT 0 CHECK (granted_total >= 0),
    consumed_total BIGINT NOT NULL DEFAULT 0 CHECK (consumed_total >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, reward_key)
);

CREATE TABLE roll_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    roll_key VARCHAR(128) NOT NULL CHECK (btrim(roll_key) <> ''),
    title_snapshot VARCHAR(128) NOT NULL,
    category VARCHAR(64) NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL,
    turn_id UUID REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    attempt_id UUID REFERENCES generation_attempts(id) ON DELETE RESTRICT,
    invalidated_at TIMESTAMPTZ,
    invalidation_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_roll_history_conversation ON roll_history(conversation_id, created_at DESC) WHERE invalidated_at IS NULL;
CREATE INDEX idx_roll_history_success_key ON roll_history(conversation_id, success, roll_key) WHERE invalidated_at IS NULL;

CREATE TABLE challenge_runs (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL UNIQUE,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    challenge_version_id UUID NOT NULL REFERENCES challenge_versions(id) ON DELETE RESTRICT,
    outcome_rule_id UUID NOT NULL,
    outcome_key VARCHAR(128) NOT NULL,
    category VARCHAR(16) NOT NULL CHECK (category IN ('success', 'failure', 'neutral')),
    title_snapshot VARCHAR(128) NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    settlement_detail JSONB NOT NULL CHECK (jsonb_typeof(settlement_detail) = 'object'),
    turn_id UUID REFERENCES conversation_turns(id) ON DELETE RESTRICT,
    attempt_id UUID REFERENCES generation_attempts(id) ON DELETE RESTRICT,
    settled_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_challenge_runs_conversation
        FOREIGN KEY (conversation_id, player_id, challenge_version_id)
        REFERENCES conversations(id, player_id, challenge_version_id) ON DELETE RESTRICT,
    CONSTRAINT fk_challenge_run_outcome_version
        FOREIGN KEY (outcome_rule_id, challenge_version_id)
        REFERENCES challenge_outcome_rules(id, challenge_version_id) ON DELETE RESTRICT
);
CREATE INDEX idx_challenge_runs_player ON challenge_runs(player_id, settled_at DESC);
CREATE INDEX idx_challenge_runs_version ON challenge_runs(challenge_version_id);

CREATE TABLE reward_ledger (
    balance_after BIGINT NOT NULL CHECK (balance_after BETWEEN 0 AND 9007199254740991),
    actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    request_id UUID UNIQUE,
    request_hash VARCHAR(64) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    public_note TEXT NOT NULL DEFAULT '',
    related_entry_id UUID REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    id UUID PRIMARY KEY,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reward_key VARCHAR(64) NOT NULL CHECK (reward_key IN ('fatecraft.points','fatecraft.message-rewrite-credit')),
    operation VARCHAR(16) NOT NULL CHECK (operation IN ('grant', 'consume', 'refund', 'reverse')),
    quantity_delta BIGINT NOT NULL CHECK (quantity_delta <> 0 AND quantity_delta BETWEEN -9007199254740991 AND 9007199254740991),
    source_type VARCHAR(64) NOT NULL,
    source_id VARCHAR(160) NOT NULL,
    challenge_run_id UUID REFERENCES challenge_runs(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT reward_ledger_operation_sign CHECK (
        (operation IN ('grant', 'refund') AND quantity_delta > 0)
        OR (operation IN ('consume', 'reverse') AND quantity_delta < 0)

    )
);
ALTER TABLE reward_ledger ADD CONSTRAINT reward_ledger_related CHECK ((operation IN ('refund', 'reverse')) = (related_entry_id IS NOT NULL));
CREATE UNIQUE INDEX reward_ledger_one_refund ON reward_ledger(related_entry_id) WHERE operation = 'refund';
CREATE UNIQUE INDEX reward_ledger_one_reversal ON reward_ledger(related_entry_id) WHERE operation = 'reverse';
CREATE INDEX idx_reward_ledger_player ON reward_ledger(player_id, created_at DESC, id DESC);
CREATE INDEX idx_reward_ledger_source ON reward_ledger(source_type, source_id);
CREATE INDEX idx_reward_ledger_run ON reward_ledger(challenge_run_id);

CREATE UNIQUE INDEX reward_ledger_source_identity ON reward_ledger(player_id,reward_key,operation,source_type,source_id);
ALTER TABLE reward_ledger ADD CONSTRAINT reward_ledger_admin CHECK (
 (source_type IN ('admin_grant','admin_reversal') AND actor_id IS NOT NULL AND request_id IS NOT NULL AND length(request_hash) = 64 AND btrim(reason) <> '')
 OR (source_type NOT IN ('admin_grant','admin_reversal') AND actor_id IS NULL AND request_id IS NULL AND request_hash = '' AND reason = '')
);
ALTER TABLE reward_ledger ADD CONSTRAINT reward_ledger_admin_operation CHECK (
 (source_type <> 'admin_grant' OR operation = 'grant') AND (source_type <> 'admin_reversal' OR operation = 'reverse')
);
ALTER TABLE player_reward_balances ADD CONSTRAINT reward_balance_totals CHECK (
 balance = granted_total - consumed_total + refunded_total - reversed_total
 AND balance <= 9007199254740991 AND granted_total <= 9007199254740991 AND consumed_total <= 9007199254740991
 AND refunded_total <= 9007199254740991 AND reversed_total <= 9007199254740991
);
-- +goose StatementBegin
CREATE FUNCTION validate_reward_related_entry() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original reward_ledger%ROWTYPE;
BEGIN
 IF NEW.related_entry_id IS NULL THEN RETURN NEW; END IF;
 SELECT * INTO original FROM reward_ledger WHERE id = NEW.related_entry_id FOR UPDATE;
 IF NOT FOUND OR original.player_id <> NEW.player_id OR original.reward_key <> NEW.reward_key OR original.quantity_delta <> -NEW.quantity_delta THEN
  RAISE EXCEPTION 'related reward entry mismatch';
 END IF;
 IF NEW.operation = 'refund' AND original.operation = 'consume' AND original.source_type = 'reward_exchange' AND original.reward_key = 'fatecraft.points' AND NEW.source_type = 'reward_exchange_refund' THEN
  RETURN NEW;
 END IF;
 IF NEW.operation = 'refund' AND (original.operation <> 'consume' OR original.source_type NOT IN ('message_rewrite','message_regenerate') OR original.reward_key <> 'fatecraft.message-rewrite-credit' OR NEW.source_type <> 'message_rewrite_refund') THEN
  RAISE EXCEPTION 'only rewrite or exchange consumption can be refunded';
 END IF;
 IF NEW.operation = 'reverse' AND (original.operation <> 'grant' OR original.source_type <> 'admin_grant' OR NEW.source_type <> 'admin_reversal') THEN
  RAISE EXCEPTION 'only admin grant can be reversed';
 END IF;
 RETURN NEW;
END; $$;
-- +goose StatementEnd
CREATE TRIGGER reward_related_entry_valid BEFORE INSERT ON reward_ledger FOR EACH ROW EXECUTE FUNCTION validate_reward_related_entry();

-- +goose StatementBegin
CREATE FUNCTION reject_reward_ledger_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'reward_ledger is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_reward_ledger_append_only
    BEFORE UPDATE OR DELETE ON reward_ledger
    FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();

-- +goose StatementBegin
CREATE FUNCTION reject_campaign_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'campaign_events is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_campaign_events_append_only
    BEFORE UPDATE OR DELETE ON campaign_events
    FOR EACH ROW EXECUTE FUNCTION reject_campaign_event_mutation();

-- +goose StatementBegin
CREATE FUNCTION reject_campaign_action_commit_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'campaign_action_commits is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_campaign_action_commits_append_only
    BEFORE UPDATE OR DELETE ON campaign_action_commits
    FOR EACH ROW EXECUTE FUNCTION reject_campaign_action_commit_mutation();

-- 对话与 Campaign 的 llm_route 创建后不可修改；路由切换必须新建对象。
-- +goose StatementBegin
CREATE FUNCTION reject_llm_route_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.llm_route IS DISTINCT FROM OLD.llm_route THEN
        RAISE EXCEPTION 'llm_route is immutable, create a new conversation or campaign instead';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_conversations_llm_route_immutable
    BEFORE UPDATE ON conversations
    FOR EACH ROW EXECUTE FUNCTION reject_llm_route_change();
CREATE TRIGGER trg_campaigns_llm_route_immutable
    BEFORE UPDATE ON campaigns
    FOR EACH ROW EXECUTE FUNCTION reject_llm_route_change();

CREATE TABLE challenge_run_rewards (
    challenge_run_id UUID NOT NULL REFERENCES challenge_runs(id) ON DELETE RESTRICT,
    ledger_id UUID NOT NULL UNIQUE REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL CHECK (quantity BETWEEN 1 AND 9007199254740991),
    reward_key_snapshot VARCHAR(64) NOT NULL CHECK (reward_key_snapshot = 'fatecraft.points'),
    name_snapshot VARCHAR(128) NOT NULL,
    unit_snapshot VARCHAR(32) NOT NULL DEFAULT '',
    icon_snapshot VARCHAR(64) NOT NULL DEFAULT '',
    balance_after BIGINT NOT NULL CHECK (balance_after BETWEEN 0 AND 9007199254740991),
    PRIMARY KEY (challenge_run_id)
);

COMMENT ON TABLE users IS '平台用户主记录，仅保存登录无关的展示资料与平台权限，不保存任何玩法角色或状态';
COMMENT ON TABLE auth_identities IS '用户与 Linux DO 等外部身份提供方的绑定记录；外部身份不能单独创建站内用户';
COMMENT ON TABLE auth_sessions IS '登录会话，只保存随机令牌的 SHA-256 摘要和失效时间';
COMMENT ON TABLE oauth_login_flows IS 'OAuth 授权过程使用的一次性 state、purpose、绑定用户、PKCE 验证信息与发起浏览器的 nonce 摘要';
COMMENT ON TABLE auth_settings IS '管理员维护的邮箱认证策略、SMTP 与验证码模板；SMTP 密码仅保存应用层密文';
COMMENT ON TABLE email_verification_codes IS '短期邮箱验证码，仅保存 bcrypt 摘要并限制尝试次数';
COMMENT ON TABLE invitation_codes IS '管理员创建的邀请码，仅保存高熵邀请码的 SHA-256 摘要';
COMMENT ON TABLE challenge_packs IS '稳定的挑战入口；运行时内容由 current_version_id 指向已发布快照';
COMMENT ON TABLE challenge_versions IS '不可原地修改的挑战版本；草稿发布时计算完整 content_hash';
COMMENT ON TABLE challenge_runtime_configs IS '挑战版本拥有的玩法引擎、状态 Schema、动作能力和玩家展示契约';
COMMENT ON TABLE challenge_assets IS '挑战版本拥有的角色、Persona、场景、Prompt 与世界书 JSON';
COMMENT ON TABLE gameplay_definitions IS '挑战版本拥有的结构化玩法目录，按 kind 与稳定 key 供可信领域动作引用';
COMMENT ON TABLE challenge_outcome_rules IS '版本化结局定义及其后端可信验证器配置';
COMMENT ON TABLE campaigns IS '长期玩法权威存档头，固定挑战版本并维护生命周期、游戏时钟和并发 revision';
COMMENT ON TABLE campaign_characters IS 'Campaign 主角的可信成长、生存资源、伤势与长期效果投影';
COMMENT ON TABLE campaign_techniques IS 'Campaign 已掌握功法及其层数、熟练度和唯一装配槽';
COMMENT ON TABLE campaign_item_stacks IS 'Campaign 可堆叠物品库存，数量变化只能由可信领域动作提交';
COMMENT ON TABLE campaign_item_instances IS 'Campaign 法宝与装备独立实例，保存耐久、绑定、来源和唯一装配槽';
COMMENT ON TABLE campaign_known_recipes IS 'Campaign 已掌握的版本化配方，不以模型上下文或摘要作为权威来源';
COMMENT ON TABLE campaign_market_states IS 'Campaign 市场补货时钟与动态价格修正；不按现实离线时间推进';
COMMENT ON TABLE campaign_market_stocks IS 'Campaign 有限市场商品池，制作和交易在 Campaign 行锁事务中更新';
COMMENT ON TABLE campaign_location_discoveries IS '玩家已发现地点及访问状态，不等同于版本中的完整地图目录';
COMMENT ON TABLE campaign_npcs IS 'Campaign 关键或实例化 NPC；私密状态与目标不得进入玩家投影';
COMMENT ON TABLE campaign_relationships IS '玩家与实例化 NPC 的多维关系和已知事实投影';
COMMENT ON TABLE campaign_faction_standings IS '玩家相对势力的声望、职位、贡献、通缉和权限投影';
COMMENT ON TABLE campaign_scheduled_events IS 'Campaign 世界事件与任务截止计划；到期处理有界，pending 行不会因事件风暴丢失';
COMMENT ON TABLE campaign_region_states IS 'Campaign 远端世界的区域级资源、危险、稳定、冲突和势力强弱聚合状态';
COMMENT ON TABLE campaign_quests IS 'Campaign 普通任务与承诺的状态机、截止游戏时间、进度和定义快照';
COMMENT ON TABLE campaign_encounters IS 'Campaign 当前遭遇的可信状态、参与者、回合和结算快照';
COMMENT ON TABLE campaign_events IS 'Campaign 只追加领域事件，visibility 控制玩家与模型可见范围';
COMMENT ON TABLE campaign_action_commits IS 'Campaign 领域动作幂等提交记录，一次动作可对应多条事件';
COMMENT ON COLUMN campaign_action_commits.narrative IS '动作的稳定 ActionResult 契约，供叙事生成、校验与诊断复用';
COMMENT ON COLUMN campaign_action_commits.state_hash IS '动作提交后权威 Campaign 状态的规范化哈希';
COMMENT ON TABLE conversations IS '玩家创建的叙事会话；每个长期 Campaign 最多绑定一个持续对话';
COMMENT ON TABLE conversation_generation_runs IS '对话可写生成的持久租约与所有权记录，用于并发互斥和进程恢复';
COMMENT ON TABLE chat_messages IS '进入模型上下文的对话消息，含 Chat/Responses 思考协议回放字段';
COMMENT ON TABLE display_messages IS '玩家可见的时间线消息及安全展示元数据';
COMMENT ON TABLE display_message_campaign_actions IS '助手叙事与同回合 Campaign 动作的来源关联，用于审计和未消费动作查询';
COMMENT ON TABLE conversation_shares IS '对话连续消息范围的不可变公开快照、所有权和撤销状态';
COMMENT ON TABLE conversation_share_reports IS '公开对话分享的匿名举报记录和来源去重指纹';
COMMENT ON TABLE conversation_share_audit_events IS '管理员处理公开分享与举报的不可变审计事件';
COMMENT ON TABLE conversation_states IS '每个对话当前的结构化状态快照';
COMMENT ON TABLE state_revisions IS '结构化状态每次可信更新前后的审计记录';
COMMENT ON TABLE conversation_turns IS '对话内稳定排序且以完整生成流程封口的逻辑回合';
COMMENT ON TABLE generation_attempts IS '回合的一次生成尝试，固定分支纪元和已提交记忆快照';
COMMENT ON TABLE agent_steps IS '主叙事 Agent 的单次模型步骤及固定工具快照';
COMMENT ON TABLE agent_tool_invocations IS 'Agent 工具调用、幂等参数和提交结果审计';
COMMENT ON TABLE conversation_memories IS '后台 Memory Agent 提交的结构化、版本化长期叙事记忆';
COMMENT ON TABLE memory_jobs IS '异步长期记忆持久任务、租约、退避和执行结果';
COMMENT ON TABLE context_build_logs IS '每次模型上下文装配的预算、分段和引用审计';
COMMENT ON TABLE llm_settings IS '玩家自己的模型连接和生成参数；API Key 仅以 AES-GCM 密文保存且不对外返回';
COMMENT ON TABLE system_settings IS '管理员维护的平台全局玩法设置与公共模型配置（单例）';
COMMENT ON TABLE admin_audit_logs IS '首次安装、账号、奖励和配置变更的事务审计；trace_id 独立于业务 request_id';
COMMENT ON TABLE llm_request_logs IS '模型逻辑调用摘要与按需采集的有界正文；不保存认证头或 API Key';
COMMENT ON TABLE character_inquiry_threads IS '与主叙事隔离的角色状态只读问答线程';
COMMENT ON TABLE character_inquiry_messages IS '只读角色问答消息、固定快照版本与公开来源引用';
COMMENT ON TABLE character_inquiry_rate_limits IS '角色问答独立持久速率窗口，不受清空问答历史影响';
COMMENT ON TABLE player_reward_balances IS '玩家积分与剧情改写次数的余额及授予、消费、返还、撤回累计投影';
COMMENT ON TABLE roll_history IS '服务端执行的可信判定结果及当时展示标题快照';
COMMENT ON TABLE challenge_runs IS '每个挑战对话唯一的原子结算流水和结局快照';
COMMENT ON TABLE reward_ledger IS '不可变奖励账本，记录所有授予、消费和管理员调整';
COMMENT ON TABLE challenge_run_rewards IS '单局实际发放的固定积分快照及入账后余额，零积分不写入';
COMMENT ON COLUMN conversations.challenge_hash IS '创建对话时固定的已发布内容哈希';
COMMENT ON COLUMN conversations.campaign_id IS '为空表示普通单局挑战；非空时唯一绑定一个长期 Campaign';
COMMENT ON COLUMN campaigns.revision IS '每次成功领域动作递增一次，用于乐观并发控制';
COMMENT ON COLUMN campaigns.event_sequence IS 'Campaign 事件序列游标，与动作 revision 分开维护';
COMMENT ON COLUMN campaigns.state_hash IS '当前权威状态的规范化哈希，叙事写入前必须匹配';
COMMENT ON COLUMN campaigns.random_seed IS '服务端生成的存档随机种子，不通过普通 API 暴露';
COMMENT ON COLUMN users.password_hash IS '邮箱密码的 bcrypt 摘要，永不保存明文密码';
COMMENT ON COLUMN users.username IS '站内登录用户名：2-32 个字符，不含 @、空白和控制字符；按 lower(username) 大小写不敏感唯一，与展示昵称 display_name 分开保存';
COMMENT ON COLUMN users.role IS '平台角色：user、admin 或唯一的 owner；普通注册不会自动提权';
COMMENT ON COLUMN oauth_login_flows.browser_binding_hash IS '发起授权时下发给浏览器的 HttpOnly nonce Cookie 的 SHA-256；回调必须带回同一 nonce，防止授权结果被其他浏览器使用';
COMMENT ON COLUMN system_settings.installation_completed_at IS '首次安装向导完成时间；为空表示实例尚未初始化';
COMMENT ON COLUMN llm_settings.api_key_encrypted IS '玩家模型 API Key 的 AES-GCM 密文，明文只存在于单次服务端调用内存';
COMMENT ON COLUMN llm_settings.reasoning_effort IS '叙事思考强度；空串表示跟随供应商默认，非空时原样透传（如 low/medium/high/ultra）；记忆与问答分别使用 memory/inquiry_reasoning_effort，互相独立';
COMMENT ON COLUMN system_settings.public_llm_api_key_encrypted IS '平台公共模型 API Key 的 AES-GCM 密文，永不通过 API 返回';
COMMENT ON COLUMN system_settings.llm_proxy_enabled IS '是否让后端 LLM 请求显式使用保存的 HTTP/HTTPS 出站代理；关闭时显式直连且忽略环境代理变量';
COMMENT ON COLUMN system_settings.llm_proxy_url IS 'LLM 出站代理地址，仅 http/https、主机和端口，不含 userinfo、query、fragment 或业务路径';
COMMENT ON COLUMN system_settings.llm_proxy_username IS '代理 Basic Auth 用户名；与密码均可留空，任一非空时发送 Proxy-Authorization';
COMMENT ON COLUMN system_settings.llm_proxy_password_encrypted IS '代理 Basic Auth 密码的 AES-GCM 密文，永不通过 API 返回';
COMMENT ON COLUMN system_settings.llm_proxy_ca_pem IS '仅追加给 LLM 专用客户端的额外可信 CA PEM；同时影响代理端和目标端 TLS 校验，不影响系统根证书池';
COMMENT ON COLUMN system_settings.llm_proxy_revision IS 'LLM 出站代理配置修订号；每次代理分组成功保存递增，独立于用户和公共模型设置修订';
COMMENT ON COLUMN reward_ledger.idempotency_key IS '调用方生成的唯一业务幂等键，防止重复授予或消费';


CREATE TABLE registration_reward_rules (
 id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
 points BIGINT NOT NULL CHECK (points BETWEEN 0 AND 9007199254740991),
 rewrite_credits BIGINT NOT NULL CHECK (rewrite_credits BETWEEN 0 AND 9007199254740991),
 actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO registration_reward_rules(points,rewrite_credits) VALUES (0,10);
ALTER TABLE users ADD COLUMN registration_reward_revision BIGINT REFERENCES registration_reward_rules(id) ON DELETE RESTRICT;
CREATE INDEX users_admin_page ON users(created_at DESC, id DESC);
CREATE INDEX users_admin_username_search ON users USING gin (username public.gin_trgm_ops);
CREATE INDEX users_admin_display_search ON users USING gin (display_name public.gin_trgm_ops);
CREATE INDEX users_admin_email_search ON users(lower(email));
CREATE UNIQUE INDEX reward_admin_request_identity ON admin_audit_logs(request_id) WHERE action LIKE 'reward_%' AND request_id <> '';

CREATE TABLE reward_exchange_offers (
    id UUID PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind VARCHAR(16) NOT NULL DEFAULT 'reward' CHECK (kind IN ('cdk', 'reward')),
    tutorial TEXT NOT NULL DEFAULT '' CHECK (length(tutorial) <= 20000),
    cost_quantity BIGINT NOT NULL CHECK (cost_quantity BETWEEN 1 AND 9007199254740991),
    reward_quantity BIGINT NOT NULL CHECK (reward_quantity BETWEEN 1 AND 9007199254740991),
    max_redemptions_per_user BIGINT NOT NULL DEFAULT 0 CHECK (max_redemptions_per_user BETWEEN 0 AND 9007199254740991),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE reward_exchange_revisions (
    offer_id UUID NOT NULL REFERENCES reward_exchange_offers(id) ON DELETE RESTRICT,
    revision BIGINT NOT NULL,
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (offer_id, revision)
);
INSERT INTO reward_exchange_offers(id,name,description,cost_quantity,reward_quantity,max_redemptions_per_user,enabled,sort_order)
VALUES ('00000000-0000-0000-0000-000000000001','剧情改写 1 次','使用积分兑换剧情改写次数。',100,1,0,TRUE,0);
INSERT INTO reward_exchange_revisions(offer_id,revision,snapshot,actor_id)
SELECT id,revision,to_jsonb(reward_exchange_offers),NULL FROM reward_exchange_offers;

CREATE TABLE reward_exchange_orders (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL UNIQUE,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    offer_id UUID NOT NULL,
    offer_revision BIGINT NOT NULL,
    units BIGINT NOT NULL CHECK (units BETWEEN 1 AND 9007199254740991),
    cost_quantity BIGINT NOT NULL CHECK (cost_quantity BETWEEN 1 AND 9007199254740991),
    reward_quantity BIGINT NOT NULL CHECK (reward_quantity BETWEEN 1 AND 9007199254740991),
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    cost_ledger_id UUID NOT NULL UNIQUE REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    reward_ledger_id UUID UNIQUE REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (offer_id, offer_revision) REFERENCES reward_exchange_revisions(offer_id, revision) ON DELETE RESTRICT
);
CREATE INDEX reward_exchange_orders_player_page ON reward_exchange_orders(player_id, created_at DESC, id DESC);
CREATE INDEX reward_exchange_orders_player_offer ON reward_exchange_orders(player_id, offer_id) INCLUDE (units);
CREATE INDEX reward_exchange_orders_offer_page ON reward_exchange_orders(offer_id, created_at DESC, id DESC);
CREATE INDEX reward_exchange_orders_admin_page ON reward_exchange_orders(created_at DESC, id DESC);
-- 每条库存记录对应一份商品；总量 = available + redeemed，剩余 = available，实时聚合不设计数器。
CREATE TABLE reward_exchange_stock (
    id UUID PRIMARY KEY,
    offer_id UUID NOT NULL REFERENCES reward_exchange_offers(id) ON DELETE RESTRICT,
    code VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'redeemed', 'removed', 'voided')),
    order_id UUID REFERENCES reward_exchange_orders(id) ON DELETE RESTRICT,
    player_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((status IN ('redeemed', 'voided')) = (order_id IS NOT NULL AND player_id IS NOT NULL))
);
CREATE UNIQUE INDEX reward_exchange_stock_code ON reward_exchange_stock(offer_id, code) WHERE code <> '';
CREATE INDEX reward_exchange_stock_pick ON reward_exchange_stock(offer_id, status, created_at, id);
CREATE INDEX reward_exchange_stock_player ON reward_exchange_stock(player_id, offer_id) WHERE status = 'redeemed';
CREATE INDEX reward_exchange_stock_order ON reward_exchange_stock(order_id);

-- 工单：每个 CDK 订单最多一条，被拒后再次发起追加到同一工单的记录中。
CREATE TABLE reward_exchange_tickets (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL UNIQUE REFERENCES reward_exchange_orders(id) ON DELETE RESTRICT,
    player_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(16) NOT NULL CHECK (status IN ('open', 'rejected', 'refunded')),
    refund_ledger_id UUID UNIQUE REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((status = 'refunded') = (refund_ledger_id IS NOT NULL))
);
CREATE INDEX reward_exchange_tickets_page ON reward_exchange_tickets(updated_at DESC, id DESC);
CREATE TABLE reward_exchange_ticket_entries (
    id UUID PRIMARY KEY,
    ticket_id UUID NOT NULL REFERENCES reward_exchange_tickets(id) ON DELETE RESTRICT,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('request', 'reject', 'refund')),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    body TEXT NOT NULL CHECK (length(body) <= 20000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX reward_exchange_ticket_entries_ticket ON reward_exchange_ticket_entries(ticket_id, created_at, id);

-- 积分 CDK：按批次生成，码只能用一次，同一账号同一批次只能兑换一次。
CREATE TABLE points_cdk_batches (
    id UUID PRIMARY KEY,
    name VARCHAR(128) NOT NULL DEFAULT '',
    points BIGINT NOT NULL CHECK (points BETWEEN 1 AND 9007199254740991),
    quantity INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 1000),
    expires_at TIMESTAMPTZ NOT NULL,
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE points_cdks (
    code CHAR(32) PRIMARY KEY,
    batch_id UUID NOT NULL REFERENCES points_cdk_batches(id) ON DELETE RESTRICT,
    redeemed_by UUID REFERENCES users(id) ON DELETE RESTRICT,
    redeemed_at TIMESTAMPTZ,
    ledger_id UUID UNIQUE REFERENCES reward_ledger(id) ON DELETE RESTRICT,
    UNIQUE (batch_id, redeemed_by),
    CHECK ((redeemed_by IS NULL) = (redeemed_at IS NULL) AND (redeemed_by IS NULL) = (ledger_id IS NULL))
);
CREATE INDEX points_cdks_batch ON points_cdks(batch_id);

CREATE INDEX reward_audit_page ON admin_audit_logs(created_at DESC, id DESC) WHERE action LIKE 'reward_%';
CREATE INDEX reward_audit_ledger ON admin_audit_logs((metadata->>'ledger_id'), created_at, id) WHERE metadata ? 'ledger_id';

COMMENT ON TABLE registration_reward_rules IS '只追加注册赠送规则修订，新账号使用最新修订';
COMMENT ON TABLE reward_exchange_offers IS '管理员配置的账号积分兑换商品与当前修订';
COMMENT ON TABLE reward_exchange_revisions IS '只追加兑换商品修订快照与操作者';
COMMENT ON TABLE reward_exchange_orders IS '只追加兑换订单，支付与所得账本在同一事务提交；CDK 商品无所得账本';
COMMENT ON TABLE reward_exchange_stock IS '兑换商品逐份库存：CDK 明文或站内奖励份额，状态 available/redeemed/removed/voided';
COMMENT ON TABLE reward_exchange_tickets IS 'CDK 订单工单，一单一条；同意即全额退分、CDK 作废、额度退还';
COMMENT ON TABLE reward_exchange_ticket_entries IS '工单只追加的发起、拒绝与退款记录';
COMMENT ON TABLE points_cdk_batches IS '管理员批量生成的积分 CDK 批次';
COMMENT ON TABLE points_cdks IS '积分 CDK 码，单次使用，同批次每账号一次';
COMMENT ON COLUMN system_settings.exchange_tickets_enabled IS '是否允许玩家发起兑换工单；关闭后已有工单仍可见、管理员仍可处理';

CREATE TRIGGER reward_exchange_order_immutable BEFORE UPDATE OR DELETE ON reward_exchange_orders FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();
CREATE TRIGGER reward_exchange_ticket_entry_immutable BEFORE UPDATE OR DELETE ON reward_exchange_ticket_entries FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();
CREATE TRIGGER reward_exchange_revision_immutable BEFORE UPDATE OR DELETE ON reward_exchange_revisions FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();
CREATE TRIGGER registration_reward_revision_immutable BEFORE UPDATE OR DELETE ON registration_reward_rules FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();

CREATE TABLE campaign_stage_rewards (
    id UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    stage_key VARCHAR(64) NOT NULL CHECK (stage_key ~ '^[a-z][a-z0-9._-]{0,63}$'),
    name VARCHAR(128) NOT NULL CHECK (btrim(name) <> ''),
    configured_points BIGINT NOT NULL CHECK (configured_points BETWEEN 0 AND 9007199254740991),
    granted_points BIGINT NOT NULL CHECK (granted_points BETWEEN 0 AND 9007199254740991),
    disabled_reason VARCHAR(64) NOT NULL DEFAULT '',
    game_time BIGINT NOT NULL CHECK (game_time >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (campaign_id, stage_key),
    CHECK ((disabled_reason = '' AND granted_points = configured_points AND granted_points > 0)
        OR (disabled_reason = 'zero_points' AND configured_points = 0 AND granted_points = 0)
        OR (disabled_reason = 'client_llm_route' AND granted_points = 0))
);
COMMENT ON TABLE campaign_stage_rewards IS '账号级只追加阶段领奖事实，含零分和直连；不随剧情检查点回档';
CREATE TRIGGER campaign_stage_reward_immutable BEFORE UPDATE OR DELETE ON campaign_stage_rewards FOR EACH ROW EXECUTE FUNCTION reject_reward_ledger_mutation();

CREATE TABLE gameplay_imports (
    id UUID PRIMARY KEY,
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    source VARCHAR(16) NOT NULL CHECK (source IN ('upload','builtin')),
    file_name TEXT NOT NULL,
    file_hash CHAR(64) NOT NULL,
    content_hash CHAR(64) NOT NULL,
    target_slug VARCHAR(128) NOT NULL,
    strategy VARCHAR(32) NOT NULL CHECK (strategy IN ('update','replace_draft','fork')),
    pack_revision BIGINT NOT NULL DEFAULT 0,
    draft_revision BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL CHECK (status IN ('preview','imported','expired','cancelled')),
    payload JSONB CHECK (octet_length(payload::text) <= 33554432),
    preview JSONB NOT NULL,
    request_id UUID,
    result_version_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (request_id),
    CHECK ((status='preview' AND payload IS NOT NULL) OR (status<>'preview' AND payload IS NULL))
);
CREATE INDEX gameplay_imports_actor_page ON gameplay_imports(actor_id,created_at DESC,id DESC);
CREATE INDEX gameplay_imports_expiry ON gameplay_imports(expires_at) WHERE status='preview';
CREATE INDEX reward_ledger_gameplay_version ON reward_ledger((metadata->>'challenge_version_id'),created_at DESC,id DESC);
CREATE INDEX reward_ledger_gameplay_pack ON reward_ledger((metadata->'gameplay_source'->>'pack_id'),created_at DESC,id DESC);

-- Emergency policies are checked at transactional write boundaries in every instance.
-- Shared version locks allow concurrent games; policy writes wait for committed actions.
-- +goose StatementBegin
CREATE FUNCTION guard_gameplay_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    row_data JSONB;
    version_id UUID;
    bound_hash TEXT;
    version_row challenge_versions%ROWTYPE;
    pack_row challenge_packs%ROWTYPE;
BEGIN
    IF TG_OP = 'DELETE' THEN row_data := to_jsonb(OLD); ELSE row_data := to_jsonb(NEW); END IF;
    IF TG_TABLE_NAME IN ('conversations','campaigns') AND TG_OP='UPDATE' THEN
        IF NEW.challenge_version_id IS DISTINCT FROM OLD.challenge_version_id OR NEW.challenge_hash IS DISTINCT FROM OLD.challenge_hash THEN
            RAISE EXCEPTION 'FC_GAMEPLAY_BINDING_INVALID';
        END IF;
    END IF;
    IF TG_TABLE_NAME = 'conversations' THEN
        IF TG_OP = 'UPDATE' AND NEW.branch_epoch = OLD.branch_epoch
           AND NEW.status NOT IN ('active','ended') THEN RETURN NEW; END IF;
        IF TG_OP = 'UPDATE' AND NEW.branch_epoch = OLD.branch_epoch AND NEW.status = OLD.status THEN RETURN NEW; END IF;
        version_id := NEW.challenge_version_id; bound_hash := NEW.challenge_hash;
    ELSIF TG_TABLE_NAME = 'campaigns' THEN
        version_id := NEW.challenge_version_id; bound_hash := NEW.challenge_hash;
    ELSIF row_data ? 'conversation_id' THEN
        SELECT challenge_version_id, challenge_hash INTO version_id,bound_hash FROM conversations WHERE id=(row_data->>'conversation_id')::uuid;
    ELSIF row_data ? 'campaign_id' THEN
        SELECT challenge_version_id, challenge_hash INTO version_id,bound_hash FROM campaigns WHERE id=(row_data->>'campaign_id')::uuid;
    END IF;
    IF version_id IS NULL THEN RAISE EXCEPTION 'FC_GAMEPLAY_BINDING_INVALID'; END IF;
    IF TG_OP = 'INSERT' AND TG_TABLE_NAME IN ('conversations','campaigns') THEN
        SELECT p.* INTO pack_row FROM challenge_packs p JOIN challenge_versions v ON v.challenge_pack_id=p.id WHERE v.id=version_id FOR SHARE OF p;
        IF pack_row.status <> 'active' OR pack_row.current_version_id IS DISTINCT FROM version_id THEN RAISE EXCEPTION 'FC_GAMEPLAY_ENTRY_CHANGED'; END IF;
    END IF;
    SELECT * INTO version_row FROM challenge_versions WHERE id=version_id FOR SHARE;
    IF version_row.run_policy = 'blocked' THEN RAISE EXCEPTION 'FC_VERSION_BLOCKED'; END IF;
    IF version_row.status NOT IN ('published','retired') OR btrim(version_row.content_hash) IS DISTINCT FROM bound_hash THEN RAISE EXCEPTION 'FC_GAMEPLAY_BINDING_INVALID'; END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER gameplay_conversation_guard BEFORE INSERT OR UPDATE ON conversations FOR EACH ROW EXECUTE FUNCTION guard_gameplay_write();
CREATE TRIGGER gameplay_campaign_guard BEFORE INSERT OR UPDATE ON campaigns FOR EACH ROW EXECUTE FUNCTION guard_gameplay_write();

-- +goose StatementBegin
DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'conversation_states','state_revisions','module_action_commits','roll_history',
        'chat_messages','display_messages','challenge_runs','conversation_generation_runs',
        'campaign_runtime_states','campaign_characters','campaign_techniques','campaign_item_stacks',
        'campaign_item_instances','campaign_known_recipes','campaign_market_states','campaign_market_stocks',
        'campaign_location_discoveries','campaign_npcs','campaign_relationships','campaign_faction_standings',
        'campaign_scheduled_events','campaign_region_states','campaign_quests','campaign_encounters',
        'campaign_events','campaign_action_commits','campaign_stage_rewards','campaign_checkpoints',
        'display_message_campaign_actions'
    ] LOOP
        IF table_name IN ('conversation_generation_runs','campaign_events','campaign_action_commits','campaign_stage_rewards','challenge_runs','module_action_commits','roll_history','state_revisions') THEN
            EXECUTE format('CREATE TRIGGER gameplay_write_guard BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION guard_gameplay_write()',table_name);
        ELSE
            EXECUTE format('CREATE TRIGGER gameplay_write_guard BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION guard_gameplay_write()',table_name);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Keep revision locks separate from business rows. Deferred triggers acquire
-- them only after business writes, avoiding a job -> conversation lock inversion
-- against branch replacement (conversation -> job).
CREATE TABLE conversation_activity_versions (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    version BIGINT NOT NULL DEFAULT 0,
    transaction_id BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_activity_latest_memory_job ON memory_jobs(conversation_id, created_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION record_activity_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    conversation UUID;
    changed BOOLEAN;
BEGIN
    IF TG_TABLE_NAME = 'conversations' THEN
        conversation := COALESCE(NEW.id, OLD.id);
    ELSE
        conversation := COALESCE(NEW.conversation_id, OLD.conversation_id);
    END IF;
    -- A cascading delete needs no snapshot; its owning REST action removes the
    -- page. Schema qualification also isolates integration-test schemas.
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I.conversations WHERE id = $1)', TG_TABLE_SCHEMA)
        INTO changed USING conversation;
    IF NOT changed THEN RETURN NULL; END IF;
    EXECUTE format('INSERT INTO %I.conversation_activity_versions AS v
        (conversation_id, version, transaction_id) VALUES ($1, 1, txid_current())
        ON CONFLICT (conversation_id) DO UPDATE
        SET version = v.version + 1, transaction_id = txid_current()
        WHERE v.transaction_id <> txid_current() RETURNING true', TG_TABLE_SCHEMA)
        INTO changed USING conversation;
    IF changed THEN
        PERFORM pg_notify('fatecraft_activity_' || substr(md5(TG_TABLE_SCHEMA), 1, 16), conversation::text);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- Evaluate stable columns before enqueueing deferred UPDATE work. Explicit
-- comparisons avoid serializing entire rows (including large message bodies)
-- and keep lease heartbeats / phase-only writes out of the deferred queue.
CREATE CONSTRAINT TRIGGER activity_conversation AFTER INSERT ON conversations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_conversation_update AFTER UPDATE ON conversations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (ROW(OLD.status, OLD.is_processing, OLD.branch_epoch, OLD.llm_route)
        IS DISTINCT FROM ROW(NEW.status, NEW.is_processing, NEW.branch_epoch, NEW.llm_route))
    EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_turn AFTER INSERT OR DELETE ON conversation_turns
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_turn_update AFTER UPDATE ON conversation_turns
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (ROW(OLD.status, OLD.invalidated_at, OLD.turn_seq)
        IS DISTINCT FROM ROW(NEW.status, NEW.invalidated_at, NEW.turn_seq))
    EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_attempt AFTER INSERT OR DELETE ON generation_attempts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_attempt_update AFTER UPDATE ON generation_attempts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (ROW(OLD.status, OLD.attempt_no, OLD.branch_epoch, OLD.committed_mutating_tool_count)
        IS DISTINCT FROM ROW(NEW.status, NEW.attempt_no, NEW.branch_epoch, NEW.committed_mutating_tool_count))
    EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_message AFTER INSERT OR DELETE ON chat_messages
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_message_update AFTER UPDATE ON chat_messages
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (ROW(OLD.role, OLD.turn_id, OLD.invalidated_at)
        IS DISTINCT FROM ROW(NEW.role, NEW.turn_id, NEW.invalidated_at))
    EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_memory_job AFTER INSERT OR DELETE ON memory_jobs
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION record_activity_change();
CREATE CONSTRAINT TRIGGER activity_memory_job_update AFTER UPDATE ON memory_jobs
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (ROW(OLD.status, OLD.target_until_turn_seq, OLD.execution_count, OLD.max_executions,
              OLD.created_at, OLD.started_at, OLD.finished_at)
        IS DISTINCT FROM ROW(NEW.status, NEW.target_until_turn_seq, NEW.execution_count, NEW.max_executions,
                             NEW.created_at, NEW.started_at, NEW.finished_at))
    EXECUTE FUNCTION record_activity_change();

-- +goose Down
-- Refuse rollback even when this file is run through an external Goose tool.
-- Raising inside the migration transaction preserves both schema and version.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'baseline 00001 is irreversible';
END;
$$;
-- +goose StatementEnd

