# 内置玩法包

每个子目录是一份平台维护、可版本化导入的完整挑战材料。玩法不限于修仙题材；目录格式、资源字段、版本规则、后端扩展边界和管理员导入流程见：

`docs/design/15-challenge-authoring-guide.md`

当前 `reincarnation-trials` 是修仙玩法的实现样例，不是所有新玩法都必须复制的领域模型。新增其他题材前，应先确认其状态、判定和结算条件是否已被后端可靠支持。

每个玩法包在 `pack.json.runtime_entry` 声明 Runtime 入口，通常指向 `runtime/runtime.json`。入口再引用 `campaign.json`（仅长期玩法）、`state-schema.json`、`actions.json`、`settlement.json` 和 `presentation.json`；导入器会把这些分层文件编译成一个版本化 Runtime 快照。

玩法包的 `runtime/presentation.json` 负责玩家界面展示元数据，并由 `runtime/runtime.json` 入口引用。固定状态字段使用 `state_sections[].fields[].label`，投影或状态中的稳定枚举使用 `value_labels`，例如：

```json
{
  "value_labels": {
    "campaign.relationship.public_stance": {
      "neutral": "中立",
      "hostile": "敌对"
    }
  }
}
```

这些标签随固定 Runtime 版本发布并进入内容哈希。不要在前端组件复制玩法词典，也不要将中文标签写入动作参数、规则或数据库事实。动态对象必须提供可读名称或显式标签；没有标签的技术 key 不会展示给玩家。

长期玩法可以在 `runtime/presentation.json` 声明 `presentation.campaign_view`，由平台通用 Renderer 读取栏目和动作：

```json
{
  "campaign_view": {
    "renderer": "generic",
    "header": {"title_path": "world.current.name", "time_path": "campaign.game_time", "time_format": "game_time"},
    "sections": [
      {"key": "character", "title": "角色", "order": 10,
        "fields": [{"path": "character.name", "label": "姓名", "widget": "title", "order": 10}]}
    ],
    "empty_states": {"character": "暂无角色资料"}
  }
}
```

`fields.path` 只能引用公开 Campaign Snapshot；数组或对象使用 `item_fields` 显式标注可读字段。`actions` 可用 `source_path + item_path` 展开嵌套集合，并以 `item.*`、`parent.*` 绑定参数。动作只能引用当前 Runtime 已允许的稳定 key，`visible_when` 只影响体验，不能替代后端授权和事务校验。新增题材只需提供自己的投影和 View，不应修改玩家端组件。
