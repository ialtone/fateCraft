# 09. 世界书与记忆设计

## 世界书

世界书用于在合适时机注入设定，不需要长期放入上下文。

世界书本身是可复用模板，但世界书激活结果只属于当前对话。

### 触发源

- 本次用户输入。
- 最近 N 条消息。
- 当前结构化状态。
- Persona 描述。
- 角色卡描述。
- 最新摘要。

### 条目类型

- 常驻条目：每次都尝试注入。
- 关键词条目：命中关键词才注入。
- 后续条目：未来支持递归触发。

### 预算

- 默认世界书预算是总 prompt 预算的 20% 到 25%。
- 常驻条目优先。
- 然后按 priority 排序。
- 超预算跳过。

### MVP 算法

```text
scanText = userInput + recentMessages + state + summary
entries = constants + keywordMatched
sort by priority
for entry in entries:
  if entry.tokens fits worldBudget:
    include
  else:
    skip and log
```

## 摘要记忆

摘要用于压缩旧历史。

摘要必须按 conversation_id 隔离。A 对话的摘要不能进入 B 对话上下文。

### 触发条件

- 历史消息超过阈值。
- 最近多次构造上下文时旧消息无法进入。
- 用户手动点击“生成摘要”。

### 摘要内容要求

摘要必须保留：

- 重要角色和关系。
- 已发生事件。
- 当前目标。
- 未解决伏笔。
- 获得或失去的物品。
- 地点和世界状态。
- 用户和 AI 达成的约定。

### 版本管理

- 每次摘要生成新版本。
- 可查看历史版本。
- 可回滚到旧版本。
- 可手动编辑当前摘要。

## 向量记忆预留

MVP 不做，但预留接口。

未来表：

- memory_chunks。
- embeddings。
- vector_index。

未来流程：

- 对摘要或关键消息生成 embedding。
- 用户输入时检索相关 chunk。
- 按 token 预算注入。
