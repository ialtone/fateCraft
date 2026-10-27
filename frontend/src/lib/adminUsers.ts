import type { AdminUser } from '@/types'

const registrationLabels: Record<string, string> = { email: '邮箱注册', linuxdo: 'LinuxDo' }

export const roleLabel = (role: string) =>
  role === 'owner' ? '拥有者' : role === 'admin' ? '管理员' : '普通用户'
export const registrationLabel = (method?: string) =>
  method ? registrationLabels[method] || method : '—'
export const formatTime = (value?: string, empty = '—') =>
  value ? new Date(value).toLocaleString() : empty

// 只有 owner 能改状态，且不能停用 owner 账号（与后端规则一致）。
export const canChangeStatus = (viewerIsOwner: boolean, target: Pick<AdminUser, 'role'>) =>
  viewerIsOwner && target.role !== 'owner'
