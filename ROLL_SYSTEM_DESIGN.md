# 天命判定系统 - 组合设计方案

## 架构设计

### 判定信息结构

AI 提供的 `roll_request`：
```json
{
  "type": "破阵判定",
  "category": "exploration",  // combat/exploration/crafting/social
  "relevant_attributes": ["悟性", "气运"],
  "attribute_weights": {"悟性": 0.6, "气运": 0.4},
  "base_difficulty": 60,
  "context": {
    "opponent": {...},
    "environment": {...},
    "advantages": [...],
    "disadvantages": [...]
  },
  "stakes": {
    "reward_value": 10000,  // 灵石价值
    "risk_level": "medium"
  },
  "description": "破解古老阵法"
}
```

### 计算流程

```
1. 基础成功率 (base_difficulty)
   ↓
2. 属性加成 (根据 attributes + weights)
   ↓
3. 天赋特质匹配 (从 state.profile.talent, state.traits)
   ↓
4. 情境修正 (context 中的各种因素)
   ↓
5. 收益平衡 (根据 stakes.reward_value 限制上限)
   ↓
6. 历史调整 (连续失败 +5~10%)
   ↓
7. 难度模式 (easy/normal/hard ±15%)
   ↓
最终成功率
```

### 多级结果

根据最终成功率分配：
```
大成功：5-10% (rolled <= 5-10)
成功：  main_probability (rolled <= threshold)
失败：  remaining
大失败：5-10% (rolled >= 95-90)
```

## 实现模块

### 1. 判定计算器 (`roll_calculator.go`)
- 核心计算逻辑
- 属性加成计算
- 情境修正计算
- 收益平衡限制

### 2. 特质匹配器 (`trait_matcher.go`)
- 从 state 中提取 talent 和 traits
- 匹配判定类型
- 返回加成/减益

### 3. 历史追踪器 (`roll_history.go`)
- 记录最近的判定历史
- 检测连续失败
- 提供动态调整建议

### 4. 结果生成器 (`roll_result.go`)
- 执行掷骰
- 确定结果等级（大成功/成功/失败/大失败）
- 格式化结果文本

## 配置化

在 `data/roll_config.yaml` 中定义：
```yaml
categories:
  combat:
    default_attributes: [根骨, 气运]
    modifiers:
      level_advantage: 15
      weapon_match: 10
      
  exploration:
    default_attributes: [悟性, 气运]
    modifiers:
      has_map: 20
      night_time: -10

reward_caps:
  - threshold: 10000
    max_success_rate: 75
  - threshold: 100000
    max_success_rate: 50
  - threshold: 1000000
    max_success_rate: 25
```

## 前端展示

判定结果消息包含详细信息：
```json
{
  "type": "roll_result",
  "roll_type": "破阵判定",
  "rolled": 37,
  "target": 52,
  "result": "success",
  "breakdown": {
    "base": 60,
    "attributes": +8,
    "talents": +15,
    "context": -10,
    "reward_cap": -15,
    "history": +5,
    "difficulty_mode": 0,
    "final": 52
  }
}
```

前端可以显示：
- 简洁版：【天命判定】破阵判定 (D100): 投出 37，目标 52，成功！
- 详细版：展开显示各项加成/减益
