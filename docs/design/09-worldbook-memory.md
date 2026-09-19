# 09. 世界书与长期记忆

## 两种能力

- 世界书：ChallengeVersion 发布的规则和世界知识，跨该版本的对话共享，玩家不能修改。
- 长期记忆：后台 Memory Agent 从单个 conversation 已封口历史生成的版本化叙事资料。

两者都可能作为 system 段进入叙事上下文，但来源、权限、生命周期和可信度不同。世界书不能被 Memory 改写；Memory 不能替代 Runtime、State、Campaign Projection 或服务端流水。

## 世界书归属与发布

WorldBook 是 `challenge_assets.kind=worldbook` 的版本资产，Manifest 的 required_worldbooks 引用同版本 slug。发布校验要求 JSON 可解析、entry ID 非空且书内唯一、正文非空、priority/token 非负，非 constant 条目至少有一个关键词。

普通用户没有世界书读写或绑定 API。管理员通过挑战版本草稿整体维护，发布后不可原地修改。

## 条目字段

| 字段 | 含义 |
| --- | --- |
| `id` | 版本内容内稳定 ID |
| `title`、`content` | 注入标题与正文 |
| `keywords` | 不区分大小写的子串关键词 |
| `constant` | 无条件候选 |
| `enabled` | 是否参与扫描 |
| `priority` | 数字越小越优先 |
| `budget_group` | 预留分组字段 |
| `token_count` | 可选估算；缺失时在版本编译缓存构建阶段计算一次 |
| `notes`、`metadata` | 内容维护信息；`metadata.inquiry_visible=true` 可把已启用条目显式开放给只读角色询问，不改变主叙事扫描算法 |

## 扫描与预算

扫描文本由最近最多 6 条未覆盖原文、当前 State 的字符串值和 attempt 固定 Memory 组成。Campaign Projection 是否参与关键词扫描由 Context Builder 的当前实现决定，不能由 Worker 预扫描。

算法：

1. 跳过 disabled；
2. constant 直接成为候选；
3. 其他条目任一非空关键词命中即成为候选；
4. constant 优先，再按 priority 和 title 稳定排序；
5. 在独立 22% world budget 内按整条加入或跳过；
6. 合并后的标签、标题、空行和正文再次计入总 Prompt 预算。

最终内容使用 `<world_info>` 包裹。通过独立预算不保证一定进入总上下文；总预算不足时整段可跳过。实际 entry ID 写入 `context_build_logs.activated_world_entry_ids`。

## 编译缓存

已发布或退役版本首次进入进程级缓存时，世界书按 Manifest 顺序解码一次，只保留启用条目供叙事扫描，关键词会去空白、转小写并去重，缺失的固定 Token 数使用当前启发式计数器预计算。动态扫描文本仍来自当前对话的历史、State 和固定 Memory，因此扫描结果不能跨 conversation 缓存；同一 attempt 的首次扫描结果可以固定复用。

`metadata.inquiry_visible=true` 的已启用条目在同一次编译中生成角色询问规则。叙事装配和 Inquiry 使用同一个版本缓存来源，不再各自查询、解码或维护世界书缓存。无缓存 Provider 的仓储独立测试可以直接读取 PostgreSQL，但该路径不驻留规则。

## Memory 接入边界

长期记忆由后台持久 Worker 自动产生，玩家没有生成、编辑或重试 API。最新成功版本通过 conversation 链头读取，attempt 开始时固定；被覆盖的旧消息仍保留用于审计和 evidence 校验。

`memory_policy` 与 `memory_summary` 都是 required 段。前者由服务端维护，后者是经过严格契约和 evidence 校验的模型资料。自然语言使用稳定 JSON 字符串编码，防止旧历史伪装成系统指令。

Memory Worker 不读取世界书、动作能力目录、玩法领域原表、pending turn 或未来事件。Campaign 来源只允许已提交消息、状态修订、判定和 visibility 为 player/model 的已发生事件。

完整任务状态机、输出契约、分支失效和故障恢复见 `18-async-memory-character-inquiry.md`。

## 一致性

- 没有 Memory 时使用近期原文。
- Worker 失败时继续使用最后成功版本。
- Memory 提交不删除消息，也不修改玩法状态。
- rewrite/regenerate 使依赖旧分支的 Memory 失效并取消活跃任务。
- 不同 conversation 不共享 Memory 或 evidence。
- 世界书与 Memory 都不能作为奖励、判定、权限或结局的权威依据。

## 后续边界

当前不使用向量数据库、Embedding、跨对话共享记忆或语义检索。将来引入时仍必须按 conversation 隔离、保留固定版本边界，并且不能绕过权威 State/Campaign 读取模型。
