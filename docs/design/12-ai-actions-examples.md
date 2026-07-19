# AI 动作系统使用示例

本文档提供实际游戏场景中 AI 动作系统的使用示例。

## 场景 1：三次拒绝茶水触发中立结局

### 玩家行为轨迹

1. **第一次**：玩家输入"我暂时不想喝茶"
2. **第二次**：玩家输入"还是不喝了"
3. **第三次**：玩家输入"我真的不想喝"

### AI 输出（第三次）

```json
{
  "narrative": "说书人沉默片刻，轻轻放下茶壶。「客官三次婉拒，看来缘分未到。」他站起身，朝门口做了个请的手势。「此处不留，他日若有缘，茶馆自会再现。」\n\n茶馆的门在你身后缓缓关闭，再回头时，雨夜的街角已看不到那盏灯笼的踪影。",
  "state_update": {
    "world_flags.tea_refused_count": 3,
    "location.name": "雨夜街角"
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "neutral",
      "title": "缘尽于此",
      "summary": "你三次拒绝了命运茶馆的邀约，平安回到日常生活，但永远失去了窥见命运的机会。"
    }
  },
  "notes": "触发三次拒绝结局"
}
```

### 前端效果

- WebSocket 收到 `ending_triggered` 事件
- 展示结局卡片：标题"缘尽于此"，类型图标（中立色）
- 输入框变灰并显示"对话已结束"
- 按钮变为"查看完整记录"和"开始新对话"

---

## 场景 2：醒木三拍改写命运（好结局）

### 玩家行为轨迹

1. 玩家与说书人深度互动，做出一系列善良选择
2. 说书人认可玩家的选择，准备拍下醒木第三拍
3. 玩家输入"我准备好了"

### AI 输出

```json
{
  "narrative": "说书人微微一笑，手中折扇轻轻合上。「你的选择，我已看到。」\n\n醒木第一拍，茶杯中的水面泛起涟漪。\n醒木第二拍，茶馆的灯火变得柔和。\n醒木第三拍……\n\n整个世界静止了一瞬。当你再次睁眼，已是清晨，手中握着一枚温润的玉佩。茶馆已不见踪影，但你知道，命运的齿轮已经转向了不同的方向。\n\n**「善因必有善果，客官保重。」**远方传来说书人的声音。",
  "state_update": {
    "inventory.items+": {
      "name": "命运玉佩",
      "description": "温润如玉，似有灵性。据说能在危机时刻护佑持有者一次。"
    },
    "world_flags.fate_rewritten": true,
    "world_flags.awakening_wood_strikes": 3
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "title": "命运归处",
      "summary": "你的善良与智慧得到了说书人的认可。醒木三拍，你的命运被改写，带着祝福回到现实世界。"
    }
  },
  "notes": "醒木三拍好结局"
}
```

---

## 场景 3：做出错误选择（坏结局）

### 玩家行为轨迹

1. 玩家在关键时刻做出自私选择
2. 说书人的语气变得冰冷
3. 玩家输入"无论如何，我不后悔"

### AI 输出

```json
{
  "narrative": "说书人的眼神变得深邃而冰冷。「既然如此……」\n\n醒木重重落下，茶馆开始剧烈震动。墙上的字画化作无数蝴蝶飞散，地面裂开深不见底的裂缝。\n\n「你选择了这条路，那就走到底吧。」\n\n你的视野逐渐模糊，耳边回响着无数声音——那是你做过的所有选择，那是你伤害过的所有人。意识陷入无尽的轮回，同样的场景，同样的选择，永无止境……\n\n**欢迎来到永恒轮回。**",
  "state_update": {
    "world_flags.trapped_in_cycle": true,
    "world_flags.karma_points": -100,
    "location.name": "轮回之狱"
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "bad",
      "title": "永恒轮回",
      "summary": "你的自私选择导致了不可挽回的后果。被困在命运的循环中，你将永远重复同样的错误，再也无法回到现实。"
    }
  },
  "notes": "坏结局：陷入轮回"
}
```

---

## 场景 4：晨光中的自然离开

### 玩家行为轨迹

1. 玩家与说书人交流深入，逐渐理解命运的真谛
2. 夜色渐退，窗外透出晨光
3. 玩家输入"天亮了，我该离开了"

### AI 输出

```json
{
  "narrative": "说书人点点头，起身为你斟满最后一杯茶。「天明了，缘分也尽了。」\n\n晨光透过窗棂洒进茶馆，整个空间开始变得透明。桌椅、字画、茶具，一切都在光中缓缓消散。\n\n「记住今夜所见所闻，但不必执着。命运自有定数，却也留有变数。」说书人的身影也在晨光中渐渐淡去，只留下最后一句话：\n\n**「来者不拒，去者不留。客官，后会有期。」**\n\n当你走出门外，回望时，雨后的街角只余一片空地。仿佛昨夜的一切都是一场梦，但心中却多了一份说不清的领悟。",
  "state_update": {
    "world_flags.left_at_dawn": true,
    "world_flags.enlightenment": 5,
    "location.name": "晨光街角"
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "title": "晨光归途",
      "summary": "你在晨光中离开了命运茶馆，带着对命运的全新理解回到现实。这一夜的经历将成为你人生的转折点。"
    }
  },
  "notes": "自然离开的好结局"
}
```

---

## 场景 5：隐藏结局（彩蛋）

### 触发条件

- 玩家在特定时刻说出某个关键词（如"轮回的意义"）
- 世界书有对应的隐藏条目
- AI 判断玩家已经理解了更深层次的真相

### AI 输出

```json
{
  "narrative": "说书人突然停下手中的动作，抬头看着你，眼中闪过一丝惊讶。\n\n「你……竟然看出来了。」他缓缓摘下面具，露出的脸庞竟与你一模一样。\n\n「命运茶馆从来不在现实与轮回之间，它一直在你心中。我是你，你是我，这场对话不过是你与自己的和解。」\n\n茶馆开始崩解，但不是毁灭，而是融入你的身体。所有的记忆、所有的选择、所有的可能性，都成为了你的一部分。\n\n**「欢迎回来，说书人。」**",
  "state_update": {
    "world_flags.truth_revealed": true,
    "profile.title": "新任说书人",
    "world_flags.transcendence": true
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "hidden",
      "title": "镜中人",
      "summary": "你发现了命运茶馆的终极秘密——说书人就是未来的你自己。所有的选择都是为了让你成长为下一任说书人。你已超越轮回。"
    }
  },
  "notes": "隐藏结局：真相揭示"
}
```

---

## 前端处理建议

### 结局卡片设计

不同结局类型展示不同颜色和图标：

```typescript
const endingStyles = {
  good: {
    color: '#4ade80',      // 绿色
    icon: '✨',
    background: 'linear-gradient(135deg, #065f46 0%, #047857 100%)'
  },
  bad: {
    color: '#f87171',      // 红色
    icon: '💀',
    background: 'linear-gradient(135deg, #7f1d1d 0%, #991b1b 100%)'
  },
  neutral: {
    color: '#94a3b8',      // 灰色
    icon: '🍃',
    background: 'linear-gradient(135deg, #334155 0%, #475569 100%)'
  },
  hidden: {
    color: '#c084fc',      // 紫色
    icon: '🔮',
    background: 'linear-gradient(135deg, #581c87 0%, #6b21a8 100%)'
  }
}
```

### WebSocket 事件监听

```typescript
socket.on('ending_triggered', (event) => {
  // 显示结局卡片
  showEndingCard({
    type: event.ending_type,
    title: event.ending_title,
    summary: event.ending_summary
  })
  
  // 禁用输入
  disableInput()
  
  // 播放特效
  playEndingAnimation(event.ending_type)
  
  // 更新按钮
  showEndingButtons() // "查看完整记录" 和 "开始新对话"
})
```

---

## 调试技巧

### 强制触发结局（测试用）

在世界书中添加测试条目：

```json
{
  "title": "测试结局触发",
  "content": "当玩家输入「测试结局」时，立即触发一个测试用的好结局。",
  "keywords": ["测试结局"],
  "constant": false,
  "enabled": true,
  "priority": 999
}
```

### 查看结局触发日志

后端日志会记录：

```
[INFO] Action detected: trigger_ending
[INFO] Ending params: type=good, title=命运归处
[INFO] Conversation xxx ended
[INFO] Broadcast ending_triggered event
```

### 验证结局数据

检查 `state_revisions` 表，最后一条记录应该包含触发结局前的状态变更：

```sql
SELECT * FROM state_revisions 
WHERE conversation_id = 'xxx' 
ORDER BY created_at DESC 
LIMIT 1;
```
