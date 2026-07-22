# 12A. AI 输出与动作示例

本文用“飞升试炼”的真实场景说明结构化信封、状态补丁、两阶段判定和结局结算。字段定义以 `12-ai-actions.md` 为准，题材数值和路线规则以 `14-ascension-trial-gameplay.md` 及对话固定的已发布挑战版本为准。

示例是维护和测试参考，不表示模型输出后业务事实一定成立。后端仍会执行路径白名单、判定、对话归属、奖励规则和事务校验。

## 1. 无判定的普通回合

玩家在坊市购买一瓶疗伤丹。价格和物品转移已经在剧情中确定，不需要随机判定：

```json
{
  "narrative": "你付出二十枚灵石，从药铺掌柜手中接过一瓶回春丹。瓶塞开启时，一缕清苦药香逸散出来。",
  "state_update": {
    "attributes.灵石": 180,
    "inventory.items+": {
      "id": "healing-pill-basic",
      "name": "回春丹",
      "quantity": 1,
      "description": "用于缓解普通伤势的丹药。"
    }
  },
  "notes": "确定性交易，不发起判定"
}
```

维护要点：

- `narrative` 与 `state_update` 必须描述同一件已经发生的事。
- 点号路径修改嵌套值，`+` 后缀向数组追加元素。
- 后端只应用允许的顶层命名空间；越界字段会被跳过。
- 当前灵石仍是模型维护的结构化状态，未来高价值奖励应改由经济账本承载。

## 2. 突破的第一阶段

玩家修为达到瓶颈并消耗筑基丹尝试突破。第一阶段只叙述准备和冲击瓶颈，不能提前宣布成功：

```json
{
  "narrative": "你服下筑基丹，药力沿经脉奔涌。多年积累被引向丹田，开始冲击那层迟迟未破的壁障。成败尚待天命裁定。",
  "state_update": {
    "profile.age": 30,
    "profile.remaining_lifespan": 108,
    "inventory.items": [],
    "cultivation.breakthrough_attempts": 1
  },
  "roll_request": {
    "type": "筑基突破判定",
    "category": "cultivation",
    "relevant_attributes": ["根骨", "悟性", "气运"],
    "attribute_weights": {
      "根骨": 0.5,
      "悟性": 0.3,
      "气运": 0.2
    },
    "base_difficulty": 72,
    "sides": 100,
    "description": "从练气期突破至筑基期",
    "context": {
      "advantages": ["服用辅助丹药", "充分准备"],
      "disadvantages": ["首次尝试"],
      "environment": []
    },
    "stakes": {
      "reward_value": 50000,
      "risk_level": "high"
    }
  },
  "notes": "第一阶段只消耗资源并申请判定"
}
```

后端处理顺序：

1. 保存第一阶段叙事和 `roll_request` metadata。
2. 应用确实已经发生的年龄、寿元、物品和尝试次数变化。
3. 从服务端状态读取属性和 traits，结合情境、收益上限、历史与管理员难度计算成功率。
4. 服务端掷骰，把结果写入 `roll_history` 和内部 system 消息。
5. 新建第二阶段消息，再次调用模型生成结果叙事。

`context` 的词条必须来自 `roll_config.yaml`。未知词条即使解析成功也不会获得加成；严格 Schema 开启时，上游通常会在生成阶段被枚举约束。

## 3. 判定后的第二阶段

假设服务端返回筑基成功，第二阶段应服从该结果，并且不能再次提交 `roll_request`：

```json
{
  "narrative": "壁障终于破碎，散乱灵力凝成稳固道基。你踏入筑基初期，神识与生机随之增长。",
  "state_update": {
    "profile.cultivation_level": "筑基期",
    "profile.cultivation_stage": "初期",
    "profile.cultivation_progress": 0,
    "profile.lifespan": 200,
    "profile.remaining_lifespan": 188,
    "cultivation.failed_breakthroughs": 0
  },
  "notes": "严格依据服务端成功结果更新境界"
}
```

若结果是普通失败，可以减少进度、寿元和资源；若是大失败，可以增加“重伤” trait 或风险。模型必须依据系统结果决定后果，不能把失败改写为成功。

第二阶段再次返回 `roll_request` 时，编排器会忽略该请求，防止判定递归。第二阶段允许提交 `trigger_ending`，因此飞升路线可以在最后一次成功判定后直接进入结算。

## 4. 虚空裂缝路线

### 穿越请求

进入裂缝时先发起真实判定：

```json
{
  "narrative": "你确认退路已经消失，随即踏入虚空裂缝。狂暴空间之力从四面八方挤压而来，护体灵光剧烈摇晃。",
  "state_update": {
    "ascension.void_rift_discovered": true
  },
  "roll_request": {
    "type": "虚空裂缝穿越判定",
    "category": "cultivation",
    "relevant_attributes": ["根骨", "气运"],
    "attribute_weights": {
      "根骨": 0.7,
      "气运": 0.3
    },
    "base_difficulty": 60,
    "sides": 100,
    "description": "穿越虚空裂缝",
    "context": {
      "advantages": ["空间感知敏锐", "充分准备"],
      "disadvantages": ["空间之力狂暴"],
      "environment": []
    },
    "stakes": {
      "reward_value": 500000,
      "risk_level": "extreme"
    }
  }
}
```

### 成功后的动作

第二阶段收到服务端成功结果后：

```json
{
  "narrative": "最猛烈的空间乱流从身后远去，前方终于出现稳定天光。你挣脱此界束缚，成功破碎虚空。",
  "state_update": {},
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "reward_key": "void_ascension",
      "title": "破碎虚空",
      "summary": "你通过虚空裂缝离开凡界，带着此生积累踏入上界。"
    }
  },
  "notes": "成功判定流水已经由后端保存"
}
```

结算服务要求当前对话至少存在一条类型前缀为 `虚空裂缝穿越判定` 的成功流水。只在 narrative 中声称穿越成功不能满足条件。

## 5. 正统渡劫路线

九波雷劫应分别发起判定，类型保持稳定前缀并明确波次，例如：

```json
{
  "narrative": "第九道劫雷在云海中凝成刺目雷柱。你祭出最后一张雷符，准备承受最终一击。",
  "state_update": {
    "inventory.items": []
  },
  "roll_request": {
    "type": "渡劫判定-第九波",
    "category": "cultivation",
    "relevant_attributes": ["根骨", "气运"],
    "attribute_weights": {
      "根骨": 0.6,
      "气运": 0.4
    },
    "base_difficulty": 45,
    "sides": 100,
    "description": "抵挡第九波天雷",
    "context": {
      "advantages": ["雷符护身", "持有护体法宝"],
      "disadvantages": [],
      "environment": ["开阔地形"]
    },
    "stakes": {
      "reward_value": 1000000,
      "risk_level": "extreme"
    }
  }
}
```

第九波成功后提交：

```json
{
  "narrative": "雷柱贯穿山巅，却没能击碎你的道基。劫云散去，接引仙光自天外垂落，九九天劫至此尽数渡过。",
  "state_update": {},
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "reward_key": "orthodox_ascension",
      "title": "渡劫飞升",
      "summary": "你渡过九九天劫，循接引仙光飞升上界。"
    }
  }
}
```

后端要求筑基、结丹、元婴、化神四次突破成功，并精确验证 `渡劫判定-第一波` 到 `渡劫判定-第九波` 九种成功流水。重复第一波或使用相似前缀不能补足资格。

## 6. 仙缘传送路线

剧情满足隐藏任务、NPC 馈赠或遗迹传承条件后，必须先请求服务端记录仙缘获取资格：

```json
{
  "narrative": "接引仙令在你面前浮起，但其上因果尚未落定。你伸手触向令牌，等待仙缘择主。",
  "state_update": {},
  "roll_request": {
    "type": "仙缘获取判定",
    "category": "exploration",
    "relevant_attributes": ["气运", "悟性"],
    "attribute_weights": { "气运": 0.7, "悟性": 0.3 },
    "base_difficulty": 35,
    "sides": 100,
    "description": "确认接引仙缘归属"
  }
}
```

只有服务端返回成功后，下一次回复才可写入 `ascension.ascension_token_owned` 和飞升符物品。之后玩家主动使用已经取得的飞升符时，不需要再掷一次使用判定：

```json
{
  "narrative": "你最后确认符中接引法阵完整，随后注入灵力。仙光包裹周身，凡界山河在脚下迅速远去。",
  "state_update": {
    "ascension.ascension_token_owned": false,
    "inventory.items": []
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "reward_key": "affinity_ascension",
      "title": "仙缘飞升",
      "summary": "你催动飞升符，经接引法阵平安抵达上界。"
    }
  }
}
```

后端要求当前持久化境界至少为结丹，并精确验证筑基突破、结丹突破和 `仙缘获取判定` 三种成功流水。具体隐藏任务是否按内容设计完成仍不是独立服务端任务引擎，因此获取判定只能作为权威资格凭据，不能替代后续任务账本。

## 7. 主动坐化

寿元将尽时，玩家可以选择体面结束此生。坐化不是挑战成功，不能携带 `reward_key`：

```json
{
  "narrative": "你将洞府与遗物安排妥当，在晨光中闭目入定。此生未能飞升，却也不再强求最后一场豪赌。",
  "state_update": {},
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "neutral",
      "title": "寿终正寝",
      "summary": "你坦然接受寿元终点，为这一世修行留下完整句点。"
    }
  }
}
```

后端会写入 result 为 `neutral`、奖励为 0 的 `challenge_run` 并结束对话。当前 neutral 不增加成功次数或失败次数；只有 `bad` 增加账号失败次数。

## 8. 寿元耗尽或陨落

寿元降至 0 时使用 bad 结局：

```json
{
  "narrative": "最后一缕生机散去，你的神魂再也无法维持。漫长修行至此终结，洞府中只余一室寂静。",
  "state_update": {
    "profile.remaining_lifespan": 0
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "bad",
      "title": "寿元耗尽",
      "summary": "你未能在寿元耗尽前突破或飞升，此世修行以坐化告终。"
    }
  }
}
```

渡劫失败、致命战斗或禁术反噬也可使用 `bad`，但 title 应准确描述原因。bad 和 neutral 一样不能申请奖励。

## 9. 会被拒绝的结算

### 成功结局缺少奖励键

```json
{
  "type": "trigger_ending",
  "params": {
    "ending_type": "good",
    "title": "破碎虚空"
  }
}
```

结果：结算拒绝，因为 good 结局缺少 `reward_key`。

### 标题与奖励规则不匹配

```json
{
  "type": "trigger_ending",
  "params": {
    "ending_type": "good",
    "reward_key": "void_ascension",
    "title": "仙缘飞升"
  }
}
```

结果：结算拒绝，因为 `void_ascension` 固定要求标题“破碎虚空”。

### 非成功结局申请奖励

```json
{
  "type": "trigger_ending",
  "params": {
    "ending_type": "neutral",
    "reward_key": "affinity_ascension",
    "title": "寿终正寝"
  }
}
```

结果：结算拒绝，active 对话不会结束，也不会记账。前端收到 `error` 事件。

### 缺少服务端判定流水

模型直接提交 `orthodox_ascension`，但当前对话没有足够的成功雷劫流水。

结果：结算事务回滚。摘要、叙事文本、客户端伪造字段或其他对话的流水均不能补足条件。

## 10. 前端事件顺序

普通生成通常经历：

```text
generation_started
generation_delta * N
message_created
state_update（仅有状态变化时）
generation_finished
```

带判定的回合分为两个独立的生成消息：

```text
第一阶段 generation_started / delta
第一阶段 message_created / state_update / generation_finished
判定结果 message_created（展示类型 roll_result）
第二阶段 generation_started / delta
第二阶段 message_created / state_update / generation_finished
ending_triggered（仅结算成功时）
```

`ending_triggered` 包含：

```json
{
  "type": "ending_triggered",
  "conversation_id": "对话 UUID",
  "ending_type": "good",
  "ending_title": "破碎虚空",
  "ending_summary": "你通过虚空裂缝离开凡界。",
  "spirit_stones": 400,
  "survival_turns": 86
}
```

前端收到后标记对话 ended、停止生成并展示结局。`spirit_stones` 是后端计算后的本次奖励，不应从模型 narrative 或 action 参数读取。

## 11. 回归检查清单

修改 Schema、Prompt、判定或奖励时，至少验证：

1. 普通叙事的合法状态路径会落库，非法路径被跳过。
2. 第一阶段资源消耗只应用一次。
3. 判定结果来自服务端并写入当前对话流水。
4. 第二阶段不能递归发起新判定。
5. 三条成功路线使用准确的 `reward_key` 和 title。
6. bad/neutral 不携带奖励键且奖励为 0。
7. 同一对话并发或重复触发结局只能产生一条 `challenge_run`。
8. 其他用户的 token、对话 ID 或判定流水不能参与本局结算。
9. 管理员发布新版本后，旧对话仍按其固定版本规则结算。
10. 文档示例中的情境词条仍存在于 `roll_config.yaml`。
