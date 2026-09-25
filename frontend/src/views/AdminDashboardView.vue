<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { NTag } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()

const areas = [
  { to: '/admin/users', title: '用户与权限', description: '管理角色、账号状态和 owner 转交。' },
  { to: '/admin/settings', title: '全局设置', description: '调整判定系统、平台 LLM 和诊断策略。' },
  { to: '/admin/auth', title: '账号与注册', description: '控制登录、注册、SMTP 和邀请码。' },
  {
    to: '/admin/identities',
    title: '第三方账户',
    description: '审计站内账号与 LinuxDo 等外部身份的绑定。',
  },
  { to: '/admin/logs', title: '诊断日志', description: '查看模型请求与上下文构建明细。' },
  { to: '/admin/gameplay', title: '玩法能力', description: '查看可信玩法模块与奖励定义。' },
  { to: '/admin/content', title: '内置内容', description: '导入仓库内置挑战内容。' },
  { to: '/admin/challenges', title: '挑战内容', description: '维护挑战包、草稿版本和发布版本。' },
  { to: '/admin/shares', title: '分享治理', description: '处理公开分享和待处理举报。' },
]
</script>

<template>
  <div class="dashboard page">
    <div class="page-title">
      <div>
        <h2>管理后台</h2>
        <p class="page-sub">平台运营入口与内容发布工具。</p>
      </div>
      <NTag :type="auth.isOwner ? 'warning' : 'info'">{{ auth.isOwner ? 'owner' : 'admin' }}</NTag>
    </div>
    <div class="area-grid">
      <RouterLink v-for="area in areas" :key="area.to" :to="area.to" class="area-link">
        <strong>{{ area.title }}</strong>
        <span>{{ area.description }}</span>
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.page {
  max-width: 1080px;
  margin: 0 auto;
}

.page-title {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 28px;
}

h2 {
  margin: 0;
}

.page-sub {
  margin: 7px 0 0;
  color: var(--text-faint);
  font-size: 13px;
}

.area-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.area-link {
  display: flex;
  min-height: 88px;
  flex-direction: column;
  justify-content: space-between;
  gap: 10px;
  padding: 16px;
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  color: var(--text);
  background: color-mix(in srgb, var(--bg-soft) 55%, transparent);
}

.area-link:hover {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.area-link span {
  color: var(--text-faint);
  font-size: 12px;
  line-height: 1.6;
}

@media (max-width: 620px) {
  .area-grid {
    grid-template-columns: 1fr;
  }
}
</style>
