# 02. SillyTavern 可借鉴功能

本项目不直接复制 SillyTavern，但可以借鉴它在长对话、角色扮演和 Prompt 工程上的成熟设计。

## 必须借鉴

### 1. 上下文窗口

SillyTavern 不会把完整聊天历史全量发给模型，而是按 token 预算从最近历史向前填充。这个设计必须采用。

落地方式：

- 保存完整历史。
- 请求时由 `ContextBuilder` 构造上下文。
- 必选 Prompt、状态、摘要、世界书和最近历史按优先级进入。
- 记录 token 明细。

### 2. Prompt Manager 思路

SillyTavern 的 Prompt Manager 允许用户启用、禁用、排序、覆盖不同 Prompt。我们需要简化版。

可借鉴点：

- Prompt 分段。
- 每段 Prompt 有角色：system/user/assistant。
- 每段 Prompt 有启用状态和排序。
- 支持全局默认 Prompt 和对话级覆盖。
- 提供 token 消耗显示。

不照搬点：

- 不做过于复杂的 UI 拖拽细节，MVP 可以用上下移动按钮。
- 不做所有 provider 的特殊格式转换，先做 OpenAI-compatible。

### 3. World Info / 世界书

SillyTavern 的 World Info 是长期设定注入的核心。我们需要保留：关键词触发、常驻条目、预算、优先级。

MVP 借鉴：

- 条目关键词。
- 常驻条目。
- token 预算百分比。
- 优先级排序。
- 扫描最近历史、用户输入、状态摘要。

后续借鉴：

- 递归触发。
- sticky/cooldown。
- depth 插入。
- inclusion group。

### 4. 角色卡和 Persona

SillyTavern 的角色卡、用户 Persona 是角色扮演体验核心。

落地方式：

- Character 保存姓名、描述、性格、场景、首条消息、示例对话、系统提示覆盖。
- Persona 保存用户身份描述。
- 对话绑定 Character、Persona、Scenario、PromptPreset。

### 5. 消息编辑和重新生成

SillyTavern 的实际使用体验很大程度来自“可修正”。这比简单聊天重要。

需要做：

- 编辑任意消息。
- 删除消息后重建后续上下文。
- 重新生成最后一条 AI 回复。
- 继续生成上一条 AI 回复。
- 保存 regenerate 记录，后续可扩展为分支。

### 6. Token Counter 和上下文明细

SillyTavern 会显示 prompt token 使用情况。我们需要一个更清晰的上下文检查器。

显示：

- 总上下文上限。
- 预留回复 token。
- 每个 PromptSegment token。
- 世界书命中条目。
- 历史消息进入数量。
- 被裁剪数量。
- 摘要版本。

## 可以借鉴但后置

### Quick Reply

可作为快捷动作按钮，例如“观察四周”“检查背包”“继续前进”。MVP 后做。

### Slash Commands

SillyTavern 的 slash command 很强，但实现复杂。可先做少量命令：

- `/regen`
- `/continue`
- `/state`
- `/summary`
- `/context`

### Regex / 文本处理

可用于清洗模型输出、移除不需要的前后缀、修复格式。MVP 先做固定处理规则。

### Attachments

可以允许用户上传纯文本资料进入当前对话或世界书。暂不做图片、视频、音频。

### Vector Memory

SillyTavern 有 vectors 扩展。MVP 暂不做，但数据库和模块设计要预留。

## 不借鉴或暂不做

- TTS。
- Stable Diffusion。
- Gallery。
- 观摩/共享播放。
- Horde 等大量 provider 适配。
- 复杂插件系统。
