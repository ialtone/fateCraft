-- +goose Up
-- 管理员用户管理：模型调用日志按用户归属，便于按用户筛选日志与查看已采集的对话。
-- conversations(player_id) 与 challenge_runs(player_id) 已有索引（idx_conversations_player_updated、idx_challenge_runs_player），此处不重复创建。
ALTER TABLE llm_request_logs ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX idx_llm_request_logs_user ON llm_request_logs(user_id, created_at DESC, id DESC) WHERE user_id IS NOT NULL;
-- 历史日志按所属对话的玩家回填；对话已被清理的历史日志保持 NULL。
UPDATE llm_request_logs l SET user_id = c.player_id
FROM conversations c
WHERE l.conversation_id = c.id AND l.user_id IS NULL;
CREATE INDEX idx_admin_audit_target ON admin_audit_logs(target_user_id, created_at DESC, id DESC) WHERE target_user_id IS NOT NULL;
-- last_login_at 此前从未落库；用仍保留的最近一次会话时间做尽力回填，之后由登录流程持续维护。
UPDATE users u SET last_login_at = s.last_at
FROM (SELECT user_id, max(created_at) AS last_at FROM auth_sessions GROUP BY user_id) s
WHERE u.id = s.user_id AND u.last_login_at IS NULL;
COMMENT ON COLUMN llm_request_logs.user_id IS '发起调用的玩家；用户删除后置空';

-- +goose Down
DROP INDEX IF EXISTS idx_admin_audit_target;
DROP INDEX IF EXISTS idx_llm_request_logs_user;
ALTER TABLE llm_request_logs DROP COLUMN IF EXISTS user_id;
