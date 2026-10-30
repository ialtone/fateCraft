# Shared 目录

平台公用内容。角色、Persona、场景目录仍为空占位，不参与运行时读取。

平台还会为每个玩法包自动注入公用 Prompt（`prompts/common.json` 的 `shared_prompt` 分段）：禁止直角引号「」『』，强调与引用短句用 Markdown `**加粗**`。包内不必复制该段；改公用文件即可作用于全部玩法。

官方挑战包本体仍在 `data/preset_packs/*`，由 `internal/challengeimport` 导入。
