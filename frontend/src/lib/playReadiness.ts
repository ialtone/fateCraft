import type { LLMSettings } from '../types/api'

/** 配置检查不调用模型；直连密钥只在设备上检查，不随创建请求上传。 */
export function playConfigurationIssue(settings: LLMSettings | null, hasLocalKey: boolean): string {
  if (!settings) return '无法读取模型配置，请重新检查后再开始游戏。'
  const client = settings.client_route_enabled && settings.llm_route === 'client'
  if (!client && settings.access_mode === 'platform_managed') {
    return settings.configuration_ready ? '' : '平台模型尚未配置完成，请联系管理员。'
  }
  if (!settings.base_url.trim() || !settings.model.trim()) {
    return '请先前往设置，填写 API 地址并选择模型，再开始游戏。'
  }
  if (client && !hasLocalKey) {
    return '本设备尚未保存 API Key，请先前往设置完成配置，再开始游戏。'
  }
  if (!client && !settings.has_api_key) {
    return '尚未配置 API Key，请先前往设置保存密钥，再开始游戏。'
  }
  return settings.configuration_ready ? '' : '模型 API 配置尚未完成，请前往设置检查并保存。'
}
