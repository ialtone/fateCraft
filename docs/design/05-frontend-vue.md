# 05. Vue 前端设计

## 技术栈

- Vue 3 Composition API 与单文件组件。
- TypeScript 负责 API、事件和 Store 类型。
- Vite 负责开发服务器、代理和生产构建。
- Pinia 保存认证、挑战、对话和运行时状态。
- Vue Router 管理登录、对话、设置和管理员路由。
- Axios 访问 REST API，原生 WebSocket 接收生成事件。
- Naive UI 提供表单、按钮、标签、弹窗和消息提示。

前端是交互层，不是安全边界。按钮隐藏、路由守卫和只读控件用于体验，真正权限必须由后端执行。

## 应用启动

应用启动后加载公开认证配置、`/api/auth/me` 和 `/api/setup/status`。路由守卫根据认证与安装状态决定跳转：

- 未确认身份：等待认证 Store 初始化。
- 未登录：除 `/login` 外跳转登录页。
- 已登录：访问 `/login` 时回到首页。
- 实例未初始化：所有非公开页面跳转 `/setup`；安装页直接创建第一个本地账号（用户名、密码、可选邮箱）并使其成为唯一 owner。
- 已初始化：再次访问 `/setup` 回到首页。
- 非管理员：访问带 `meta.admin` 的路由时回到首页。

Axios 使用 `withCredentials: true`，响应拦截器把后端 `{ error }` 转换为统一错误消息。401 应使认证 Store 清理本地用户状态。

## 路由与页面

| 路由 | 页面 | 主要职责 |
| --- | --- | --- |
| `/login` | LoginView | 用户名或邮箱登录/注册、已绑定 LinuxDo 登录、禁用方式提示 |
| `/setup` | SetupView | 空库直接创建本地用户名/密码、确认唯一 owner、关闭安装入口 |
| `/` | ConversationListView | 已发布挑战、我的对话、筛选与创建入口 |
| `/conversations/:id` | ConversationView | 消息、生成、状态、判定、结局和上下文 |
| `/shares` | ConversationListView + MySharesView | 首页“我的分享”小标签；跨对话检索、分页与管理 |
| `/rewards` | RewardsView | 积分兑换（默认）、资产概览、奖励记录、兑换记录、余额流水五个小 Tab |
| `/settings` | SettingsView | 当前用户 LLM 配置、第三方账户绑定 |
| `/admin/identities` | AdminView | 管理员只读审计站内账号与第三方身份绑定关系 |
| `/admin` | AdminLayout + AdminDashboardView | 管理后台总览；与普通用户端分离 |
| `/admin/users` | AdminView(section=users) | 用户角色、账号状态和 owner 转交 |
| `/admin/settings` | AdminView(section=settings) | 判定系统、平台 LLM 和诊断策略 |
| `/admin/auth` | AdminView(section=auth) | 登录注册策略、SMTP 和邀请码 |
| `/admin/logs` | AdminView(section=logs) | 模型、上下文、管理审计和实例诊断状态 |
| `/admin/gameplay` | AdminView(section=gameplay) | 可信玩法模块、验证器与计算器 |
| `/admin/rewards` | AdminView(section=rewards) + AdminRewardsPanel | 独立奖励管理、发放、兑换与审计 |
| `/admin/content` | AdminView(section=content) | 仓库内置挑战导入 |
| `/admin/challenges` | AdminView(section=challenges) | 挑战包、草稿和发布版本 |
| `/admin/shares` | AdminView(section=sharing) | 公开分享和举报治理 |

Character、WorldBook 和 Prompt 不再拥有普通用户独立路由。旧页面、Store、组件和 API 均应删除，不能只从导航隐藏。

## 主导航

已登录用户固定显示首页、奖励和设置；`auth.isAdmin` 为真时额外显示管理入口。进入 `/admin/*` 后隐藏普通用户顶栏，使用独立的 `AdminLayout` 侧栏导航。顶栏可以展示奖励定义中标记为 featured 的少量余额，但不能写死灵石、回溯次数等题材字段。用户菜单展示当前名称并提供退出登录。

导航不读取或缓存角色权限以外的管理数据。管理员身份仍以 `/auth/me` 返回的服务端用户为准。

## 首页与创建流程

首页同时承载挑战发现和对话管理：

- 挑战区域显示名称、图标、版本、简介、标签、功能和奖励概要。
- 对话区域按 active、ended、archived 筛选，支持标题搜索。
- active 对话显示进入、归档和删除等适用操作。
- ended 对话只读，不显示恢复入口；版本更新自动封存的对话同样只读，并提供基于当前版本继续的入口。

`CreateConversationModal` 打开时加载已发布挑战，默认选中指定挑战或列表第一项。切换挑战后必须重新选择该挑战允许的 Persona。提交体只包含 `challenge_slug`、`persona_slug` 和非空标题。

## 对话页面

页面进入时并行或按依赖顺序加载：

- 对话元数据。
- 展示消息。
- 当前玩家可见状态。
- 当前挑战的公开 Runtime Presentation 与玩家能力。
- 当前用户奖励余额。
- 左栏角色问答独立历史（进入“询问”视图时加载）。
- WebSocket 连接。

只有 active 对话显示输入和生成操作。ended 或 archived 对话显示只读状态提示；普通对话可从只读页面调用 fork，在当前玩法版本创建新对话。

流式输出按 `operation_id + attempt_id + message_id` 聚合，并按事件 ID 去重、按 sequence（合并 delta 使用 `payload.sequence_start` 到 `sequence` 的覆盖范围）拒绝倒序和检测真实缺口。带有操作/attempt 身份的迟到消息、状态和 Campaign 事实事件，如果不属于当前操作会直接丢弃。`agent.reasoning.delta` 写入临时 assistant 的思考区，思考期间自动展开；首个 `agent.text.delta` 到达时收起。时间线由 Chat Store 的内容变更通知驱动滚动，只在用户贴近底部时跟随，并覆盖思考折叠动画的最终高度；用户主动上翻后不抢回位置。收到最终 `message_created` 和 `agent.turn.completed` 后以服务端消息替换临时内容；收到 `agent.turn.failed/cancelled` 时清除未持久化增量，收到 `agent.turn.partial` 时保留已提交状态并提示继续叙事。`state_update` 更新状态，`challenge_completed` 使用 Runtime 的 completion presentation 展示通用完成弹窗并刷新奖励。

重新生成、续写和编辑改写的 REST 响应为 `202 Accepted`，只代表操作已被 durable run 接收，不代表分支已经替换或模型已经完成。前端不得在 202 后删除 assistant、截断时间线或扣除本地奖励；必须等待带操作身份的 `branch.replaced`/终态事件，或通过快照重新读取确认。按钮的 `actionPending` 必须保持到终态、明确 HTTP 错误或快照确认后才解除，不能在收到 202 的瞬间释放。

`roll_result` 消息默认只显示服务端生成的判定摘要；当 `metadata.details_version` 为已支持版本时，`MessageItem` 通过安全解析器显示默认收起的“判定详情”。详情中的公开前置条件、成功率分解、最终成功率和结果区间都是判定时冻结的快照，前端不重新计算概率或读取当前状态。解析器只接收 `public=true` 的条件，旧消息、缺失详情或未知版本安全降级为摘要；模型/内部条件及原始技术路径不得进入 DOM。

状态面板按 Runtime Presentation 的分组、字段标签、控件、单位和 `value_labels` 渲染，只读取后端返回的玩家投影。未知控件安全降级为文本，未声明字段不按英文 key 自动展示；动态对象中的技术 key 必须省略或使用玩法包声明的标签。玩法专属值不得在 Vue 组件维护词典，平台固定枚举才允许放在共享 platform label helper 中。消息改写按钮及其文案、成本来自平台级 `platform_capabilities`，不能由前端写死。

左侧 `CharacterSidebar` 使用“状态 / 询问”双视图。`InquiryPanel` 只向专用询问 API 发送自然语言问题，展示独立历史、公开来源标签、发送/取消状态、快照过时提示和清空操作；它不复用主叙事输入、不伪造状态，也不把回答写回状态面板。

长期存档对话使用 `CampaignViewRenderer`。栏目、字段、空状态、可见条件和动作槽全部来自当前固定 Runtime 的 `presentation.campaign_view`，组件只负责通用布局、控件渲染和提交稳定 action key；不再维护修仙栏目、状态词典或动作函数。`CampaignPanel` 仅保留兼容壳，未声明视图的旧 Runtime 安全降级为状态面板。

普通对话和 Campaign 共用 `GuidanceRenderer`。玩法包可在 `presentation.guidance.groups` 声明有界的 `message`（发送普通用户输入）或 `action`（提交已白名单动作）指引；普通挑战的可见项由后端依据 player state 投影，前端只按 placement 渲染并通过现有输入/动作入口提交。正文中的“选项 1/2/3”不会被解析成按钮，指引条件也不等同于权限。

收到 `state_update` 时仅在这个明确边界重新读取 Runtime Presentation，以便服务端投影的隐藏项随状态变化出现或消失；流式正文增量不会触发 Runtime/Guidance 重算。请求按序号丢弃陈旧响应，避免快速连续状态事件把较旧投影写回。

Campaign View 的字段路径只能读取后端公开 Snapshot，集合必须通过 `item_fields` 声明可读字段；未知技术 key 被省略。动作绑定只生成参数，不承担权限、消耗、随机和结算，后端事务会再次校验。

WebSocket 事件不是可靠队列。服务端会合并相邻同键 reasoning/text delta 以降低帧数；增量拥塞时允许丢失，控制/事实事件超时会主动断开慢连接。刷新或重连后应重新读取 REST 数据，不能仅依赖已收到事件恢复状态；检测到 attempt 内 sequence 缺口时也立即触发快照同步。

## Store 边界

### `auth`

保存公开认证配置、当前用户、首次安装状态和登录状态；提供 `isAdmin`（`admin`/`owner`）与 `isOwner` 派生值，以及邮箱、已绑定 LinuxDo、首次安装完成和退出流程。

### `challenges`

只保存普通用户可见的已发布挑战公开列表，不缓存完整版本 JSON。

### `conversations`

保存列表、当前对话和最近结局事件；封装创建、标题更新、归档、恢复、结束和删除。

### `chat`

保存当前对话展示消息与发送、编辑、重新生成、续写状态。切换对话时必须清空，不能把上一局消息带入下一局。

### `socket`

管理连接、生成中状态、流式增量和事件分发。它只保存传输状态，挑战完成结果应写入 `conversations`，奖励事件触发 `rewards` 重新读取服务端余额。

### `state`、`runtime`、`context`

分别保存当前玩家可见状态与修订记录、当前挑战公开展示契约，以及最近一次上下文构建的玩家摘要和当前活动状态。上下文检查器展示模型、Token 占用、历史/世界书数量、Memory 滞后，叙事生成、回合结算、角色询问、Memory 排队/运行状态，以及只读内容缓存的命中率、驻留版本、估算内存和淘汰/失败计数；Prompt、世界书正文、内部 ID 和裁剪原因不在该面板展示。切换对话时必须清空 runtime 与 context，并防止旧对话的异步读取覆盖新对话；普通用户没有状态写 API。

### `rewards`

保存当前用户的奖励定义与余额，提供 featured 投影和按稳定 key 查余额。奖励流水由奖励页面按需读取，不允许本地修改余额。

### `campaign`

保存当前长期存档的权威玩家快照。玩家动作使用客户端生成的唯一 `action_id` 和快照 revision 提交；执行期间禁止重复点击，成功后重新读取完整快照，revision 冲突或业务拒绝时同样刷新后再展示错误。`campaign_updated` 只用于及时更新 Campaign 头并触发重新读取，不能作为可靠库存账本。

### `inquiry`

保存当前对话的专用角色问答 thread、消息、加载/发送状态和 stale 提示。切换对话时必须清空；询问失败不能改变 `chat`、`socket`、`state` 或 `campaign` Store。

### `settings`

保存当前有效的 LLM 设置，以及 Responses/Chat Completions 协议、叙事、长期记忆、角色问答三个模型选择。协议默认 Responses；首次进入不预填叙事模型，获取模型列表后选择首项；后两者模型为空时继承叙事模型。用户处于 `user_managed` 时可编辑自己的连接和密钥；处于 `platform_managed` 时显示平台只读投影，隐藏用户 Key、保存和个人模型列表入口。API Key 只显示是否已配置，不回显密文或明文；管理员全局设置提供接入模式和公共模型配置。设置页没有 Provider 选项：填谁的 Base URL 就用谁的服务（官方、OpenRouter、中转站、本地模型均可），OpenAI、DeepSeek、OpenRouter 官方地址由服务端自动适配。“接口协议”下拉框始终提供 Chat Completions 和 Responses 两项，由用户手动选择，服务端永不改写；协议提示分两种：表单 base_url 与已保存值相同、且服务端识别档位不是兼容网关时显示“已识别为 {档位名}，已自动适配请求参数”；其他情况显示“按所选协议请求。不确定时先用‘测试连接’或能力诊断确认”。管理后台公共 LLM 配置同理：无公共 Provider 选项，协议下拉与提示规则相同，识别结果使用 `public_llm_resolved_profile`。前端不写任何按供应商区分协议的提示；DeepSeek + Chat 下记忆/问答不可用的情况，由后端拦截文案和能力诊断结果告知用户。

## 用户奖励页面

`RewardsView` 使用积分兑换、资产概览、奖励记录、兑换记录、余额流水五个小 Tab，积分兑换排在首位并作为进入奖励页的默认页。概览只展示余额/额度和藏品，奖励记录负责领取与获奖历史，商品页只展示可兑换商品及确认窗口；兑换订单与余额明细分别独立。页面顶部提供奖励刷新，每个列表保留自己的刷新、筛选和分页，切换后保留已有筛选与输入。

兑换记录中的「查询兑换结果」打开独立弹窗，校验完整请求 UUID 后查询当前账号的订单，结果及错误只展示在弹窗。查询到历史订单不当作一次新兑换，也不能清除其他请求的待确认状态；只有请求 ID 匹配才结束原待确认兑换。查询未命中不代表兑换没有执行，保留原请求用于查询或同次重试。

兑换控制器随页面挂载以恢复同标签页 sessionStorage 中的原请求，商品仅在首次进入兑换 Tab 时加载。未确认兑换显示页面级「继续确认」入口，切换 Tab 不创建新请求；确认窗口提供「查询本次结果」和「重试同次兑换」。成功后可直接跳到兑换记录，刷新余额、奖励记录与已挂载的订单/流水；余额接口失败不阻止其他记录刷新。交易编号在用户详情中默认折叠，内部账本 ID 仅管理视图显示。

本地回归运行 `npm run test:player-rewards`，覆盖页面分组、查询入口归属、请求 ID 校验及历史查询不误结束其他待确认兑换；真实浏览器联调仍需检查切换、刷新恢复、未知结果查询/重试及移动端。

## 管理页面

管理页面使用独立侧栏路由，大区域下按操作职责组织小 Tab。单一区域路由隐藏重复的大 Tab 标题，不把所有表单堆在同一小 Tab。

| 管理区域 | 小 Tab / 展示方式 |
| --- | --- |
| 总览 | 管理区域入口 |
| 用户与权限 | 用户管理、最近操作；完整审计进入诊断日志 |
| 全局设置 | 判定规则、模型服务、日志策略 |
| 账号与注册 | 登录与注册、发信邮箱、验证码邮件、邀请码 |
| 第三方账户 | 单一绑定列表 |
| 诊断日志 | 模型调用、上下文构建、管理审计、诊断状态 |
| 奖励管理 | 概览、奖励资产、通关规则、注册赠送、用户发放、兑换商品、兑换订单、发放记录、管理审计 |
| 玩法能力 | 单一可信模块目录 |
| 内置内容 | 内容列表及导入预览弹窗 |
| 挑战内容 | 挑战包与版本、版本编辑；选择版本后进入编辑页 |
| 分享治理 | 公开分享、待处理举报 |

全局和账号设置的保存按钮只提交当前分组，响应也只回填该组，其他 Tab 的未保存输入、待修改密钥及清除标记保留。SMTP 与验证码模板测试使用已保存配置。新拆分的小 Tab 首次访问时挂载，切换后保留表单；高级 JSON 未应用的文本也保留。不同挑战版本通过组件 key 隔离编辑状态。管理员权限沿用服务端边界，普通管理员不能修改平台 LLM 或认证配置。

本地回归运行 `npm run test:admin-tabs`（Node 24），验证设置分组不夹带其他页草稿/凭据，以及小 Tab 归属和保留策略；前端类型检查与生产构建继续使用 `npm run build`。

### 玩法设置

「全局设置 → 判定规则」使用 Switch 控制全局判定开关，单选按钮选择 easy/normal/hard，保存后以服务端响应回填判定字段。公共模型与日志分别在同区域的「模型服务」和「日志策略」维护。

### 玩法能力

- 只读展示后端已经注册的 engine、动作、verifier、calculator 和 settlement handler。
奖励资产维护集中在独立“奖励管理”入口。

### 奖励管理

`AdminRewardsPanel` 按上表的九个小 Tab 组织操作。通关规则与注册赠送分开，商品配置与兑换订单分开，发放记录与管理审计分开。规则列表跳转 `/admin/challenges?version=<id>` 并打开版本编辑，已发布版本只读；编辑继续创建草稿并发布。`RewardUserSearch` 调用服务端搜索和分页，`AdminUsersPanel` 使用 `{users,has_more}` 中的 users 数组，不把响应对象当成账号列表。

`AdminRewardUserPanel` 绑定选定用户 ID，分为账户余额、发放奖励、获奖记录、余额流水，默认进入发放奖励；选择资产后显示该资产余额。发放表单维护用途、直接/待领取方式、管理原因和公开说明，预览后提交稳定 request ID，切换小 Tab 不重建发放请求。`RewardRecordsPanel` 提供来源/状态/时间筛选，管理模式另支持用户和资产筛选、详情审计、撤销、受限冲正和说明；兑换来源不可单边冲正。`RewardLedgerPanel` 用游标展示实际流水。

`AdminRewardExchangePanel` 配置商品及修订；`RewardExchangePanel` 确认价格、份数和所得，未知结果保留原请求并查询或重试；`RewardExchangeOrders` 展示不可变订单。用户领取或兑换后重新读取 rewards Store、记录与流水，顶栏额度随 Store 刷新，不在本地预扣余额。概览总量用十进制字符串避免精度丢失。完整交互边界见 [22-reward-management-and-exchange.md](22-reward-management-and-exchange.md)。

### 账号与注册

- 配置邮箱登录、注册、验证码、邀请码和验证码策略。
- 配置 SMTP 发件信息、测试邮件和注册邮件模板；密码只显示是否已保存。
- 创建、停用和查看邀请码用量，邀请码原文只在创建成功时展示一次。

### 内置内容

- 列出服务器仓库中发现的内置挑战，不展示技术资产正文。
- 对比仓库版本、数据库当前版本和 SHA-256 内容哈希。
- `available` 可由管理员确认后导入并立即发布。
- `imported` 表示数据库当前版本与仓库版本、哈希一致，不重复导入。
- `update_available` 表示仓库内容与数据库当前快照不同，展示“覆盖现有内容”并要求二次确认。已使用版本应先提升版本号；发布新版本后，旧普通对话自动变为只读，详情页提供“基于新版本继续”入口。
- 前端先展示奖励差异、引用和前后规则，再提交 slug、preview_token 和显式 overwrite；预览过期重新确认，不能指定服务器路径、上传内容或绕过校验。

### 挑战内容

- 「挑战包与版本」列出挑战包及其版本和状态，「版本编辑」承载当前版本表单。
- 可创建新挑战包。
- 可从当前版本复制指定版本号的新草稿。
- 选择版本后加载完整 `manifest + runtime + assets + outcomes + reward_rules` 文档并切换到版本编辑；编辑器分为基本信息、叙事资产、玩法定义、玩法运行、状态字段、玩家界面、结局验证、奖励规则、高级 JSON。
- draft 使用结构化编辑器分区维护入口、资产、运行契约、状态、界面、结局和奖励，published 全部只读。
- “高级 JSON”用于导入、导出和少量高级维护；导入前先检查顶层结构，业务校验以服务端发布结果为准。
- 发布成功后刷新挑战列表和当前版本内容。

## 类型契约

`src/types/api.ts` 维护后端模型与 WebSocket 事件；`src/types/auth.ts` 维护认证公开类型；`src/types/rewards.ts` 维护获奖、兑换和管理响应。删除后端字段或路由时，应同时删除前端类型、Store 和页面引用，避免“类型还在但功能已不存在”。

对话创建类型不得重新加入 Character、Prompt 或 WorldBook ID。管理员完整版本类型不得复用于普通挑战公开响应。

## 错误与加载状态

- 所有提交操作应有 loading，避免重复点击。
- 读取失败显示可操作错误，不把空列表误当成“没有内容”。
- 发布失败保留编辑器内容，展示服务端具体校验原因。
- 401 进入重新登录流程，403 显示权限不足，404 不推测资源属于其他用户。
- WebSocket 错误结束生成中状态，但不删除已落库消息。

## 响应式与交互约束

- 固定工具条、对话输入区和状态面板不能因长标题或长标签错位。
- 对话动态行动提示使用内容区底部固定的 `ConversationGuidanceBar`，位于滚动消息区和输入框之间；没有 active guidance 时不占布局空间，点击只提交服务端签发的 guidance ID 与 item key。
- 移动端优先保证对话阅读和输入，辅助面板可折叠。
- 图标按钮需要 tooltip 或可访问名称。
- 危险操作应使用明确确认，不能把“结束”“归档”“删除”混成同一语义。

## 前端回归清单

- 未登录访问受保护路由会到登录页。
- 普通用户看不到管理入口且直接访问 `/admin` 或任意 `/admin/*` 子路由被守卫拦截；子页面只加载对应管理数据。
- 后台 `/admin/logs` 由 AdminDiagnostics 提供模型、上下文、管理审计和诊断状态四页，支持筛选、游标加载更多和复制关联 ID。模型摘要始终保存，正文开关默认关闭；关闭、截断、无记录、详情已清理和读取失败分别提示。配置审计展示脱敏差异。完整字段见[日志、诊断与配置审计](23-logging-and-diagnostics.md)。
- 全局设置中的详细日志支持全部、指定用户、指定对话三个范围，仅开启「模型请求详细日志」时显示采集范围；用户复用后台搜索，点击选择后显示目标，对话填写完整 UUID。切换范围清空旧目标，关闭开关隐藏范围并保留配置，保存后应用于后续模型调用，无自动关闭。范围外日志仍有摘要，正文预算与清理策略独立。
- 普通上下文检查器只显示可理解的占用摘要和当前活动；Prompt 分段、引用 ID 与裁剪诊断仅管理员可见。
- LinuxDo 登录只用于已绑定身份；首次安装直接在 `/setup` 创建本地账号，第三方绑定从设置页发起，注册作为次级入口；关闭方式提交时有明确提示。
- 创建表单切换挑战会修正无效 Persona。
- ended 对话没有输入、归档或恢复按钮。
- 状态面板由当前玩法的中文标签和展示契约驱动，不泄漏 internal/model 字段。
- 状态修订和角色询问资料同样不泄漏 internal/model 字段；询问不会触发玩法动作或阻塞主叙事。
- 重新生成和消息改写入口读取 Runtime API 返回的平台级 `platform_capabilities.message_rewrite` 与奖励余额；玩法包不声明该能力，成本和授权不能在 Vue 中硬编码。
- 奖励页余额与流水能在挑战结算或消息改写后刷新。
- 管理员已发布版本只读，草稿可保存和发布。
- 管理员玩法定义支持 `recipe` 和 `market`，发布错误能定位悬空材料、产物、技艺、地点、货币或商品引用。
- 长期存档只展示当前位置市场和持有载体对应的可学习配方；领域动作执行中不能重复提交。
- 长期存档任务与承诺分组展示状态、截止游戏时间、目标进度和奖励；接受/提交后重新读取权威快照。
- 长期存档完成后从平台保留的 `outcome_rewards` 展示本次带出、来源转换和当前账号余额，不从叙事文本猜测奖励。
- 尘缘视图只展示当前 region 聚合局势和最近已发生事件，不展示未来计划事件或 NPC 私密目标。
- 类型检查和生产构建通过。

### 长期玩法的数据来源不变量

Campaign Renderer 只能读取当前版本后端返回的 Campaign Snapshot；StatePanel 只能读取 conversation state。组件不得按字段名称猜测来源，也不得添加 `character.name || scene.character_name` 等跨域兜底。新字段必须先进入 Runtime `field_ownership`、可信 Projection 和 `campaign_view`，前端通用 renderer 自动展示；Action 成功后按 Campaign ID 重新拉取完整 Snapshot，WebSocket 只负责触发及时刷新。
## 对话分享前端

`ConversationView` 页头是唯一的新分享入口，按需加载 `ConversationShareDialog`。对话框只负责创建或替换分享，提供“全部消息”和“选择连续片段”两种模式；连续片段通过起止选择器确定范围，预览区只显示最终选中的连续消息。公开身份使用开关控制，默认匿名，打开后显示当前账号名称和头像，不提供自由编辑署名的输入框。全部消息每批渲染 100 条，生成进行中时禁止创建或替换分享。

`MySharesView` 嵌在首页对话区域，通过“我的对话 / 我的分享”小标签切换；它不出现在与奖励、设置、管理同级的顶部导航中。该视图通过 `(created_at, id)` 游标跨对话加载当前用户的分享，以“加载更多”追加结果，不请求总数或使用深 OFFSET；支持状态筛选、分享标题或来源对话标题搜索、复制、浏览、撤销和重新发布。重新发布复用 `ConversationShareDialog` 恢复原连续范围，成功后由服务端原子撤销旧链接；普通对话分享弹窗不再包含历史列表或管理标签。

`/s/:token` 与生产 HTML 外壳跳转使用的 `/shared/:token` 都是 public route。`SharedConversationView` 使用独立的 `shares` store、`SharedConversationContent` 和 `SharedMessageItem`，不会复用带编辑、reasoning 或私有状态的对话组件。公共路由在认证初始化之前放行，401 拦截器也排除 `/public/`，页面不创建对话 WebSocket。

公开 Markdown 经过专用净化：移除远程图片、iframe 和 style，危险 URL 不生成可点击链接，外链统一使用 `noopener noreferrer`。页面设置 `noindex,nofollow` 与 `no-referrer`。非匿名模式直接读取快照中的受信任 LinuxDo 头像地址，读取失败时回退为名称首字头像。

## 客户端直连前端

- `src/llm/localKey.ts`：本设备 Key 的 get/set/clear（`localStorage`，按用户隔离），不经过 pinia，不进日志；登出时清除。
- `src/stores/settings.ts`：账号设置；App.vue 按 `auth.user?.id` 监听，切换账号（含登出/会话过期）时先重置设置并断开 Relay，再加载新账号设置，防止新账号沿用旧账号的 `base_url` 与路由判断。
- `src/stores/llmRelay.ts`：应用级用户 WebSocket（`/ws/llm-relay`），满足“已登录 && 有效路由为 client”时连接（App.vue 监听启停，1s→30s 指数退避），与接入模式无关。`relay.request` 只接受白名单 endpoint，URL 由**本地** base_url 拼接；无 Key 回 `no_key`；SSE 按 UTF-8 字节 32KiB/50ms 合并回传（50ms 兜底定时器防尾部滞留），单次读取过大时拆成多个 ≤64KiB 帧、seq 连续；`bufferedAmount > 1MiB` 时背压暂停；`relay.cancel` 中止 fetch 并回 `aborted`。
- `SettingsView`：LLM 接入卡片按四种状态渲染。`platform_managed` + 开关关：只显示“模型服务已准备就绪”；`platform_managed` + 开关开：路由二选一（平台模型（推荐）/客户端直连），选平台模型时显示已就绪卡片（不显示配置表单），选直连时显示个人配置表单 + 本设备 Key + 测试连接；`user_managed` + 开关关：个人配置表单 + 服务端 API Key（无路由选择）；`user_managed` + 开关开：路由二选一（服务端转发（推荐）/客户端直连）。切换确认框文案按接入模式区分（平台模式不提“删除服务端 Key”，切回平台模型提示“无需填写 Key”）；`platform_managed` 下选服务端保存时只提交 `{ llm_route: 'server' }`。client 模式 Key 只写本设备、可浏览器获取模型列表、可测试连接（区分鉴权失败/跨域网络失败/成功，https 配 http 非回环给混合内容提示）、可清除本设备 Key；能力诊断走同一接口（服务端经 Relay 执行）。`user_managed` 下开关关闭时路由选择、本设备 Key 区与测试连接全部隐藏，保存固定提交 `server`；服务端返回 `server` 且无 Key 时在 Key 框上方提示“平台已关闭客户端直连，请填写 API Key”（`platform_managed` 下无此提示，关闭开关会自动回到平台模型）。
- 会话页：页头路由徽标（`LlmRouteTag`）+ client 路由 Relay 状态点；路由不匹配时输入/重生成/续写/改写/guidance 全禁用并给横幅（新建对话、前往设置）；平台关闭开关导致的直连对话不可用时横幅文案为“平台已关闭客户端直连……”，只保留“新建对话”按钮；client 未连接时发送禁用；`server` 路由在界面上随接入模式显示为“平台模型”或“服务端转发”（不匹配横幅、创建弹窗的“本对话将使用”）；`challenge_completed` 带 `rewards_disabled_reason` 时显示“客户端直连模式不发放奖励”，Campaign 快照带该字段时同样在视图顶部显示替代横幅。列表页同样徽标；创建弹窗只读展示本次固化路由；分享公开页对 client 快照显示未校验标记。
- 管理后台全局设置：“允许用户选择客户端直连”开关独立显示（与接入模式无关，位于公共 LLM 开关之外），只有 owner 可操作；关闭时弹确认框（中断在途回合、直连对话只读；`user_managed` 下用户需填 Key，`platform_managed` 下自动回到平台模型）。公共 LLM 能力检测走独立的 owner 接口 `POST /api/admin/system-settings/capabilities/check`，直接使用平台配置，与检测者本人的路由无关。
- 今后若给主站加 CSP，`connect-src` 必须允许用户配置的模型域名，否则客户端直连整体失效。
