# 13. 挑战内容、版本与结算

## 设计目标

挑战系统要同时满足：

- 平台可以持续发布新玩法和新版本。
- 玩家只能选择公开、安全的入口，不接触技术内容。
- 一局开始后规则不随管理员更新漂移。
- 发布内容可审计、可校验、不可原地修改。
- 成功奖励可由后端验证并且不能重复领取。

## 聚合边界

`challenge_pack` 是稳定入口，`challenge_version` 是完整快照。版本拥有：

- Manifest。
- Character 资产。
- 一个或多个 Persona 资产。
- Scenario 资产。
- Prompt 资产及其分段。
- 一个或多个 WorldBook 资产。
- Reward rules。

这些内容必须作为整体发布。不能让 Prompt 使用版本 A、世界书使用版本 B、奖励读取当前最新版本。

## Manifest

Manifest 负责版本公开元数据和资产引用，核心结构：

```json
{
  "id": "reincarnation-trials",
  "name": "飞升试炼",
  "version": "1.0.0",
  "description": "...",
  "author": "ialtone",
  "tags": [],
  "default_config": {
    "recommended_character": "heavenly-dao-proctor",
    "recommended_persona": "rogue-cultivator",
    "recommended_scenario": "heavenly-trial",
    "recommended_prompt": "ascension-trials",
    "required_worldbooks": ["ascension-system"],
    "allowed_personas": ["rogue-cultivator"]
  },
  "quick_start_guide": "...",
  "features": []
}
```

发布时 Manifest 的 id、name、description 必须与挑战包一致，version 必须与版本记录一致。recommended 资产必须存在；设置 allowed_personas 时，recommended Persona 必须位于其中。

## 生命周期

### Challenge Pack

- 新建时为 hidden，没有 current version。
- 首个版本发布后变为 active 并指向该版本。
- 后续发布只更新 current version，新对话使用新版本。
- retired 用于未来停止新玩家进入，不能破坏旧对话引用。

### Challenge Version

- `draft`：可以整体保存和反复校验。
- `published`：不可原地修改，可供新对话选择。
- `retired`：不再作为当前入口，但已有对话仍可运行。

不支持 published 回到 draft，也不支持覆盖相同 pack + version。内容修改必须创建新版本号。

## 新建与复制草稿

新挑战包的首个草稿从空 Manifest 开始，管理员需要填入完整版本文档。

已有 current version 时，新草稿复制：

- Manifest，并自动把 `manifest.version` 改为新版本号。
- 全部资产，重新生成数据库 UUID。
- 全部奖励规则，重新生成数据库 UUID。

复制只产生 draft，不影响当前发布版本。数据库 `(challenge_pack_id, version)` 唯一。

## 草稿保存

管理端提交：

```json
{
  "manifest": {},
  "assets": [],
  "reward_rules": []
}
```

保存阶段要求 Manifest 和 asset content 是合法 JSON，资产 kind、slug、name、content.id 与版本内唯一性基本有效。允许暂时缺少发布必需内容，便于分步编辑。

仓储事务锁定版本并确认仍为 draft，然后替换资产和奖励规则。保存与发布使用同一版本行锁。

## 发布校验

发布拒绝以下情况：

- Manifest 缺 id、name、version 或默认 Character/Persona/Scenario/Prompt。
- Manifest 与挑战包 id、name、description 不一致。
- Manifest version 与版本记录不一致。
- recommended 或 required 引用不存在。
- allowed Persona 不存在或不包含 recommended Persona。
- 资产 kind、slug、name、JSON 或 content.id 无效。
- 版本内出现重复 kind + slug。
- 默认 Prompt 无法解析。
- Prompt 缺少 enabled、required、system 的 `system_master`、`settlement_security` 或 `output_schema`。
- Required WorldBook 条目 ID 重复、内容为空、优先级非法，或关键词条目没有关键词。
- 没有奖励规则，reward_key 重复，结局不是有效 good 结局。
- multiplier_bps、最小成功数或 max_reward 非法。
- 成功奖励 key 没有出现在 Prompt 内容中。

校验基于事务锁定后的精确快照，避免并发保存造成“哈希对应旧内容、数据库已经是新内容”。

## 内容哈希

哈希输入包括：

- 规范化 Manifest JSON。
- 按 kind + slug 排序后的资产 kind、slug、name、content 和 sort_order。
- 按 reward_key 排序后的结局、条件、计算、上限和 enabled。

JSON 先解析再重新序列化，忽略空格和对象键顺序差异。数据库 UUID、创建时间和更新时间不进入哈希，因此同一业务内容可得到稳定 SHA-256。

发布事务把哈希写入版本并更新挑战包 current version。运行时再次用相同算法计算，必须同时满足：

```text
actual_hash == challenge_version.content_hash
conversation.challenge_hash == challenge_version.content_hash
```

## 空库首次初始化

server 连接数据库后、开放 HTTP 监听前执行内置挑战检查：

文件加载、版本文档组装和数据库导入由独立的 `internal/challengeimport` 模块负责；`internal/challenge` 不依赖文件系统，只提供领域校验与发布能力。导入模块同时提供单目录导入、全部内置内容原子导入、按 slug 管理员导入和空库启动策略，启动流程与管理员接口复用同一实现。

1. 开启事务并获取 PostgreSQL transaction-level advisory lock。
2. 在锁内统计 `challenge_packs`。
3. 只要存在任意挑战包，立即结束检查，不读取 `DATA_DIR/preset_packs`。
4. 完全为空时，按目录名排序扫描所有内置挑战。
5. 读取 pack.json 和 characters、personas、scenarios、prompts、worldbooks 下的 JSON。
6. 从 Manifest 分离 reward_rules，执行与管理员发布相同的完整校验。
7. 计算稳定内容哈希，创建 pack、draft、资产和奖励规则，再通过正常发布流程发布。
8. 所有内置包成功后提交事务；任一失败则整体回滚并阻止 server 启动。

advisory lock 防止多个后端实例在同一空库上重复初始化。检查条件是“数据库完全没有挑战包”，而不是“某个内置 slug 不存在”，因此已有自定义内容的数据库也不会被启动过程悄悄补写官方内容。

首次初始化只解决新库可直接使用的问题，不是内容同步机制。内置源只接受 `major.minor.patch` 稳定版本号；后续修改官方挑战必须提升版本号。管理员在“内置内容”中确认仓库版本与哈希后显式导入发布，导入器禁止同版本覆盖和版本降级，重启服务不会更新已存在的挑战。

## 公开投影

普通挑战接口只返回：

- pack ID、slug、名称和描述。
- 当前版本、图标、标签、功能和快速指南。
- allowed Persona 的 ID、slug、名称和公开描述。
- enabled good 奖励的 key、标题和格式化倍率。

不返回技术 Prompt、世界书正文、Character 系统指令、完整 Scenario、奖励条件或计算 JSON。

## 玩家选择与对话固定

创建对话只接受 challenge slug、persona slug 和标题。Challenge Service：

1. 读取 active pack 的 current version。
2. 验证状态与非空哈希。
3. 重算完整内容哈希。
4. 解析 Manifest 默认资产。
5. 校验 Persona 位于 allowed 集合和同一版本。
6. 返回 Selection，由 Conversation Service 固定到对话。

对话创建后 PATCH 只能改标题。数据库组合外键保证选中资产属于固定版本。

## 运行 Bundle

生成时按对话固定版本构造 Bundle：

- selected Character、Persona、Scenario 必须按 ID 找到且 kind 正确。
- Prompt 按 Manifest recommended_prompt 读取。
- WorldBook 按 required_worldbooks 顺序读取。
- published 和 retired 版本可用于旧对话。
- 任一引用或哈希不一致时返回错误，不使用仓库文件或代码内置兜底。

## 奖励规则

当前规则结构：

```json
{
  "reward_key": "void_ascension",
  "ending_type": "good",
  "ending_title": "破碎虚空",
  "conditions": {
    "min_cultivation_rank": 4,
    "required_successful_rolls": [
      "筑基突破判定",
      "结丹突破判定",
      "元婴突破判定",
      "虚空裂缝穿越判定"
    ]
  },
  "calculation": {
    "multiplier_bps": 20000
  },
  "max_reward": null,
  "enabled": true
}
```

`multiplier_bps` 以 10000 表示 1 倍，避免浮点记账。`min_cultivation_rank` 使用凡人 0、练气 1、筑基 2、结丹 3、元婴 4、化神 5、炼虚 6、合体 7、大乘 8。`required_successful_rolls` 是必须全部存在的精确服务端成功判定类型，发布时拒绝空项和重复项。

具体内置奖励见 `14-ascension-trial-gameplay.md`。

## 结算事务

模型只能提交 ending type、title、summary 和 reward_key。修仙动作 handler 在保存候选回复前，从服务端当前状态读取局内灵石、角色、境界和回合数并冻结输入；预校验失败的候选不会落库。

Challenge Service：

1. 保存前非锁定预校验固定版本规则、境界和精确判定证据。
2. 通过后保存叙事和状态，再对 conversation 加 `FOR UPDATE`。
3. 比较 authenticated player 与 conversation owner。
4. 要求 conversation 为 active。
5. good 结局要求非空 reward_key。
6. 按固定 challenge_version 查找 enabled rule。
7. 精确匹配 ending type 与 title，并重做境界和判定集合校验。
8. 使用冻结的 `base * multiplier_bps / 10000` 计算，并应用可选 max_reward。
9. 更新账号奖励和成功/失败次数。
10. 写入唯一 challenge_run。
11. 把对话设为 ended 并释放生成锁。

事务失败全部回滚。非 good 结局携带 reward_key 会被拒绝；bad 和 neutral 奖励为 0。

## 权限

- 普通用户：读取公开挑战、选择允许 Persona、创建并操作自己的对话。
- 管理员：检查并导入仓库内置版本，读取完整版本文档、创建 pack 和 draft、保存、发布、设置全局判定。
- 启动初始化器：只在空库中读取仓库材料，并走与管理端相同的领域发布规则，不属于在线 HTTP 权限。

完整版本 API 永远不能因为前端隐藏而对普通用户开放。

## 失败恢复

- 保存失败：事务回滚，保留上一个完整 draft。
- 发布校验失败：版本保持 draft，可继续修改。
- 发布并发保存：版本行锁使操作串行，后发生者看到已发布状态后被拒绝。
- 首次初始化失败：整个初始化事务回滚，server 不开始监听；修正材料后重新启动即可。
- 多实例首次启动：advisory lock 串行检查，后获得锁的实例看到已有 pack 后直接跳过。
- 运行时哈希失败：停止生成并由管理员检查数据库或重新发布新版本。
- 结算重放：返回已结算冲突，不重复记账。

## 内容演进规则

任何影响玩家规则、上下文、可选 Persona 或奖励的变化都应升级挑战版本。仅文档错别字若同步进入 Manifest 或资产 JSON，也会改变哈希，仍应使用新版本。

发布前应完成：JSON 解析、领域测试、前端类型检查、空库首次启动演练和玩法文档同步。不得在生产库直接 UPDATE published JSON 作为热修复。
