# Shared 目录

该目录不再参与运行时内容读取或管理端写入，仅保留为空目录占位。

角色、Persona、场景、Prompt、世界书和奖励规则统一存入 PostgreSQL 的挑战版本。官方内容放在 `data/preset_packs/*`，由 `internal/challengeimport` 统一负责解析、校验与导入；后端仅在 `challenge_packs` 为空的首次启动时自动发布，管理员内容通过管理端保存为数据库草稿并发布。
