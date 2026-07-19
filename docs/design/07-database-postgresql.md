# 07. PostgreSQL 数据库设计

## 设计原则

运行时数据按 `conversation_id` 隔离。对话 A 的消息、状态、摘要和上下文日志不能被对话 B 自动读取。

## 表设计

### conversations

```sql
id uuid primary key,
player_id text not null,
title text not null,
status text not null default 'active',
character_id uuid null,
persona_id uuid null,
scenario_id uuid null,
prompt_preset_id uuid null,
created_at timestamptz not null,
updated_at timestamptz not null,
ended_at timestamptz null,
archived_at timestamptz null,
deleted_at timestamptz null
```

状态值：

- active。
- ended。
- archived。
- deleted。

### chat_messages

```sql
id uuid primary key,
conversation_id uuid not null,
role text not null,
content text not null,
hidden boolean not null default false,
token_count int null,
parent_message_id uuid null,
created_at timestamptz not null,
updated_at timestamptz not null
```

### display_messages

```sql
id uuid primary key,
conversation_id uuid not null,
chat_message_id uuid null,
type text not null,
markdown text not null,
metadata jsonb not null default '{}',
created_at timestamptz not null
```

### conversation_states

```sql
conversation_id uuid primary key,
state jsonb not null,
schema_version int not null default 1,
updated_at timestamptz not null
```

### state_revisions

```sql
id uuid primary key,
conversation_id uuid not null,
source text not null,
related_message_id uuid null,
patch jsonb not null,
before_state jsonb null,
after_state jsonb not null,
created_at timestamptz not null
```

### prompt_presets

```sql
id uuid primary key,
name text not null,
description text null,
created_at timestamptz not null,
updated_at timestamptz not null
```

### prompt_segments

```sql
id uuid primary key,
preset_id uuid not null,
key text not null,
role text not null,
content text not null,
enabled boolean not null default true,
required boolean not null default false,
priority int not null default 100,
position int not null default 0
```

### world_books

```sql
id uuid primary key,
name text not null,
description text null,
created_at timestamptz not null,
updated_at timestamptz not null
```

### world_book_entries

```sql
id uuid primary key,
world_book_id uuid not null,
title text not null,
content text not null,
keywords text[] not null default '{}',
constant boolean not null default false,
enabled boolean not null default true,
priority int not null default 100,
token_count int null,
metadata jsonb not null default '{}'
```

### conversation_world_books

```sql
conversation_id uuid not null,
world_book_id uuid not null,
primary key (conversation_id, world_book_id)
```

这张表只表示某个对话选择了哪些世界书模板，不表示共享运行时记忆。

### memory_summaries

```sql
id uuid primary key,
conversation_id uuid not null,
version int not null,
content text not null,
covered_until_message_id uuid null,
token_count int null,
created_at timestamptz not null
```

### context_build_logs

```sql
id uuid primary key,
conversation_id uuid not null,
trigger_message_id uuid null,
model text not null,
max_context_tokens int not null,
reserved_response_tokens int not null,
used_prompt_tokens int not null,
segments jsonb not null,
included_message_ids uuid[] not null default '{}',
activated_world_entry_ids uuid[] not null default '{}',
created_at timestamptz not null
```

## 索引建议

- `conversations(player_id, updated_at desc)`。
- `conversations(status, updated_at desc)`。
- `chat_messages(conversation_id, created_at)`。
- `display_messages(conversation_id, created_at)`。
- `world_book_entries(world_book_id, enabled)`。
- `memory_summaries(conversation_id, version desc)`。
- `context_build_logs(conversation_id, created_at desc)`。

## 隔离约束

- 所有运行时查询必须带 conversation_id。
- ContextBuilder 只能读一个 conversation_id。
- MemorySummary 不允许跨 conversation_id 查询。
- ContextBuildLog 不允许混入其他对话的 included_message_ids。
- 只有模板数据可以跨对话复用。
