# 玩法包交换与版本生命周期

本文描述实际实现。内容包、可信模块和存档是三个不同的版本边界：内容包提供配置，服务器预装的精确 engine key/version 执行规则，存档固定创建时的版本 ID 与内容哈希。

## 管理流程

后台内容管理分为玩法包、版本管理、上传导入、导入记录、版本编辑；内置内容仍保留独立入口。版本管理可创建新版草稿、查看固定快照、导出、查看引用和奖励记录、切换新开局入口、回退、退役、暂停与解除暂停。

1. 上传 `.fatecraft.json` 或 ZIP，选择新版本导入、替换同号草稿，或以新 id 另存为独立包。
2. 服务端编译并校验完整内容，展示当前与目标版本、精确模块、稳定身份对应的内容差异、阶段/结局积分预算和当前版本活跃引用。
3. 确认创建草稿。新上传包默认 hidden，无当前入口；内置更新也只导入草稿。
4. 管理员检查编辑并发布。发布默认不切换入口；勾选同时选择新开局版本时禁止静默降级。
5. 在版本管理选择当前入口并启用包。回退旧版需要显式确认；确认窗展示前后内容、积分预算与双方活跃存档。

草稿是尚未发布的可编辑版本；不是玩家可以选择的玩法。内容修订递增，保存和发布必须带当前 revision；发布后内容不可修改。包管理修订用于入口、开关和导入预览冲突；运行策略使用单独 policy_revision。

官方启动初始化仍按 `backend/data/bootstrap.json` 在事务中补齐八类九个默认包并发布启用。已经存在的包不升级、不覆盖、不重新启用；第三方包不加入官方初始化清单。

## 可交换协议

结构 Schema 为 [fatecraft.gameplay-package.schema.json](../../backend/data/schemas/fatecraft.gameplay-package.schema.json)。完整、可编译的 JSON 示例为 [地下瞭望塔](../examples/underground-watchtower.fatecraft.json)，目录形式见 `backend/data/preset_packs/underground-watchtower`。Schema 描述结构，最终权威校验还包括模块能力、字段权限、引用、初始状态、结局和积分预算；通过 Schema 不能替代服务器校验。

| 字段 | 约定 |
| --- | --- |
| format / format_version | `fatecraft.gameplay-package` / `1` |
| runtime_contract_version | `2`，独立于内容版本和模块版本 |
| manifest | 稳定 id、名称、严格 major.minor.patch、公开说明、入口推荐和元数据 |
| runtime | 内联完整运行契约及精确 engine key/version |
| assets | kind + slug、name、content、sort_order；包含角色、玩家、场景、Prompt 和世界书 |
| definitions | kind + key 的规则定义，保留可见性、标签、内容和顺序 |
| outcomes | 结局 key、可信 verifier、展示和固定积分；便携 manifest 不重复声明 outcomes |
| content_hash | 规范化固定内容的 SHA-256；导出会计算并带上，上传可省略，提供时必须一致 |
| provenance | 作者/许可、上游 id/version/hash 和备注；作为来源摘要保存，不参与运行内容哈希 |

导出从数据库事务读取完整版本，不读取仓库源文件。文件不包含本地 UUID、用户/发布人、余额、存档、随机种子、模型记录、密钥或启用/暂停状态。数据库内容编辑后的排序和所有已参与哈希的 manifest 字段保留；另一个实例导入时分配新本地 ID，固定内容哈希不变。

ZIP 根目录包含 `pack.json`、`runtime/` 入口和引用分片、资产目录及定义文件；允许一个统一外层目录。导出另外附带 `fatecraft-package.json`，保存格式版本、来源、声明哈希、资产路径与身份/顺序。无该元数据的现有标准目录 ZIP 仍可解析；不能仅以文件名推断编辑后的排序。ZIP 和内联 JSON 最终经过同一个 Portable 编译器及发布校验。

首期不支持跨包 dependencies、脚本上传、自动安装模块或远程市场。另存为会记录上游来源；检测到 manifest.id 之外的原包身份引用时拒绝通用改写，要求作者修改源包。

## 导入一致性与资源边界

预览记录保存于 `gameplay_imports`，绑定操作者、源文件/内容哈希、目标、策略、包/草稿修订，30 分钟有效。确认在身份锁和包→版本锁顺序下再次校验内容、来源和修订，与草稿、结果记录及审计在同一事务提交。官方预览确认还复核服务器官方源哈希。

同一 request_id 与同一预览重复确认返回原结果；换预览复用 request_id 返回冲突。前端为预览使用稳定请求 ID，网络结果不明时重查或重试同次请求，刷新后也可从导入记录恢复确认。已发布同号同内容返回原版本；同号不同内容一律拒绝。草稿同内容可幂等导入，不同内容须明确替换且修订未变化。更新官方/编辑器包必须明确确认来源可信；这不允许覆盖已发布快照。

未确认内容过期或取消后清除 payload；摘要与导入结果保留。预览只创建导入记录，不改变运行包。被拒绝的解析/校验直接返回原因，不保存业务草稿。单实例最多同时执行两个上传校验，全局待确认预览最多 100 条。

上传最多 16 MiB，ZIP 解压最多 32 MiB、512 文件、单文件 2 MiB、压缩比最多 100:1。拒绝越界路径、Windows 盘符/设备/ADS、反斜线、大小写重复路径、符号链接、加密文件和不支持的文件。私有临时目录在解析结束清理。导出 ZIP 使用 Store，过大定义文件需使用 JSON 导出。JSON 拒绝重复键、未知类型字段、尾随内容、无效 UTF-8、超过 64 层嵌套、越出 JavaScript 安全整数范围或发生静默精度损失的数值；数值文本最多 128 字节，指数绝对值最多 324。

## 新局、旧局与运行暂停

| 操作 | 新开局 | 已有普通对话 / Campaign |
| --- | --- | --- |
| 发布新版本 | 默认不切换入口 | 固定原版继续，不自动归档 |
| 切换/回退入口 | 后续使用选定已发布版本 | 不迁移状态、不修改原奖励 |
| hidden 停用包 | 禁止 | allowed 且模块可用时继续 |
| retired 退役版本 | 无当前入口，不供新开局 | 固定原版继续或恢复归档 |
| blocked 暂停版本 | 禁止 | 禁止新动作、回档及生成事实提交；已提交结果可查 |
| 解除暂停 | 取决于包开关和入口 | 只恢复可执行能力，不复活已结束/归档/删除存档 |
| 缺失精确模块 | 不可开局 | 展示原因并只读，不降级到最新模块 |

创建对话/Campaign 的数据库事务检查包启用、当前指针、版本允许和绑定哈希。普通对话恢复不再要求是当前入口；固定原版本的内容校验与运行策略仍须通过。继续叙事、改写和回档不改变绑定。`fork` 是在该包当前版本重新开局，从初始状态开始，不复制旧正文或状态；Campaign 不走普通 fork。玩家版本条目展示实际版本、当前新开局版本、暂停原因和重新开局入口。

数据库写触发器对消息、状态、动作、判定、结局和 Campaign 领域数据实施最终策略检查，写事务持有版本共享锁；策略变更持有排他锁。先提交的事实保留，暂停先提交则后续写入失败，跨实例同样有效。生成心跳最多约 30 秒内取消新发现的暂停任务；取消不等于回滚已经提交的动作。已登记请求可通过原结果查询/回放，不能重新执行，也不能绕过参数和归属校验。

当前入口不可直接删除。有任何对话（包含软删除）、Campaign、奖励、导入结果或版本审计依赖时拒绝物理删除；真正未引用草稿/历史版本才可删除。账号余额与已经领取的阶段奖励不随存档回档。

## 奖励来源

奖励继续复用账号账本。服务端在发放事务内从原固定版本和真实结算记录生成 `metadata.gameplay_source`，包含包 ID/slug、发放时版本名称、实际版本 ID/版本号/哈希、关联对话/Campaign、阶段 key/name 或结局 key/title。

管理员列表和详情显示该投影，支持包及实际版本组合筛选，版本列表包括退役版本；版本管理可跳转对应奖励记录。重命名包、切换入口、禁用原版不会重写已经发放的来源。版本链接查看管理员固定快照；游戏 ID 仅用于追溯，不授予管理员读取用户私人对话正文的权限。非玩法流水不虚构玩法来源。V1 存档在 V2 发布后仍按 V1 预算结算。

## 管理接口

所有管理接口要求管理员权限；导入记录还按操作者隔离。

```text
GET    /api/admin/builtin-challenges
GET    /api/admin/builtin-challenges/:slug/preview
POST   /api/admin/gameplay-imports/preview            multipart: file,strategy,target_slug,trusted_update
POST   /api/admin/gameplay-imports/:id/confirm        {request_id}
GET    /api/admin/gameplay-imports                    ?before=time|id
GET    /api/admin/gameplay-imports/:id
DELETE /api/admin/gameplay-imports/:id
GET    /api/admin/challenge-versions/:id/export       ?format=json|zip
GET    /api/admin/challenge-versions/:id/references
PUT    /api/admin/challenges/:id/current-version      {version_id,expected_revision,confirm_rollback}
POST   /api/admin/challenge-versions/:id/retire        {expected_revision}
PUT    /api/admin/challenge-versions/:id/run-policy   {policy,reason,expected_revision}
GET    /api/conversations/:id/gameplay-version        owned conversation only
```

普通保存/发布接口带 `expected_revision`；发布另带 `select_current`（默认 false）。旧内置专用确认接口与 preview_token 流程已移除，内置预览返回统一导入记录，确认使用 gameplay-imports。文件错误返回具体可读原因；预览变化为 `preview_changed`，修订冲突为 `gameplay_revision_conflict`，暂停为 `gameplay_version_blocked`，模块不可用为 `gameplay_module_unavailable`。交换格式错误分别返回 package_format_unsupported、package_too_large、package_path_invalid、package_hash_mismatch、package_dependency_unsupported 或 package_validation_failed。已发布不可覆盖及引用保护沿用领域错误，不能用通用“联系管理员”代替。

## 部署与验收

`gameplay-check` 使用与服务器相同的 DATABASE_URL，只读检查当前入口、未结束/可恢复对话和活跃 Campaign 的精确模块与契约依赖，失败退出 1；它不执行迁移、发布、状态转换或模型调用。服务器启动也报告不兼容并保留只读访问。

结构仍只修改 `backend/migrations/00001_init.sql`。此工具不代表已有库会自动新增字段；新项目使用维护者管理的正确基线，不自动改 Goose 历史或清库。任何后续线上升级应以届时单独编写的向前迁移为准。

本轮本地结果及人工验收清单见 [模块验收记录](gameplay-modules/validation.md)。真实 PostgreSQL、浏览器和模型试玩由维护者执行；本地通过不代表这些验收已经完成。
