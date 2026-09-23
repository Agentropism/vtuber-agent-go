// 类型化 API 客户端：一个端点一个函数，字段与 docs/API.md 的契约一一对应。
//
// 错误约定：非 2xx 抛 ApiError；503 表示该能力未装配（界面显示「未启用」态），
// 405/404 来自 mux 时是纯文本，统一塞进 message。

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }

  /** 未装配（未配置对应能力）时网关返回 503。 */
  get disabled(): boolean {
    return this.status === 503
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })

  if (!res.ok) {
    const text = await res.text().catch(() => '')
    let message = `HTTP ${res.status}`
    if (text) {
      try {
        const body = JSON.parse(text) as { error?: string }
        message = body.error ?? text
      } catch {
        message = text
      }
    }
    throw new ApiError(res.status, message)
  }

  return (await res.json()) as T
}

function post<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, { method: 'POST', body: JSON.stringify(body ?? {}) })
}

// ---- 状态 ----

export interface StatusSnapshot {
  uptime: string
  started_at?: string
  clients?: { count: number; platforms: string[] }
  broadcast?: {
    enqueued: number
    played: number
    preempted: number
    dropped: number
    failed: number
  }
  upload?: { queue_len: number; queue_size: number; dropped: number }
  sessions?: number
}

export const getStatus = () => request<StatusSnapshot>('/status')

// ---- 配置 ----

export type ConfigSnapshot = Record<string, unknown>

export const getConfig = () => request<ConfigSnapshot>('/config')

// ---- 会话 ----

export interface SessionInfo {
  channel_id: string
  pending: number
  history_len: number
  last_active?: string
  last_idle?: string
}

export const getSessions = () => request<{ count: number; sessions: SessionInfo[] }>('/sessions')

export interface HistoryMessage {
  role: string
  content: string
}

export const getHistory = (channel: string) =>
  request<{ channel_id: string; messages: HistoryMessage[] }>(
    `/sessions/${encodeURIComponent(channel)}/history`,
  )

export interface SendMessagePayload {
  user_id?: string
  user_name?: string
  text: string
}

export const sendMessage = (channel: string, payload: SendMessagePayload) =>
  post<{ status: string }>(`/sessions/${encodeURIComponent(channel)}/messages`, payload)

// ---- 播报 ----

export const speak = (text: string, emotion?: string) =>
  post<{ status: string }>('/speak', { text, emotion: emotion ?? '' })

// ---- 记忆 ----

export interface MemoryEntry {
  id: number
  time: string
  channel_id: string
  user: string
  text: string
  reply: string
  weight: number
}

export const getMemory = (query: string, limit: number) => {
  const params = new URLSearchParams()
  if (query) params.set('query', query)
  params.set('limit', String(limit))
  return request<{ count: number; total: number; entries: MemoryEntry[] | null }>(
    `/memory?${params.toString()}`,
  )
}

export const addMemory = (payload: {
  channel_id?: string
  user?: string
  text: string
  reply?: string
  weight?: number
}) => post<{ id: number }>('/memory', payload)

export const deleteMemory = (id: number) =>
  request<{ status: string; id: number }>(`/memory/${id}`, { method: 'DELETE' })

// ---- 工具 ----

export interface ToolInfo {
  name: string
  description: string
  parameters?: unknown
  read_only: boolean
}

export const getTools = () => request<{ count: number; tools: ToolInfo[] }>('/tools')

export const callTool = (name: string, args: unknown) =>
  post<{ name: string; result: unknown }>(`/tools/${encodeURIComponent(name)}`, { args })

// ---- 模型 ----

export interface ModelInfo {
  name: string
  url: string
  scale: number
  x_shift?: number
  y_shift?: number
  idle_motion?: string
  expressions?: string[] | null
}

export interface ModelState {
  current: ModelInfo
  models: ModelInfo[]
  emotions: string[]
}

export const getModels = () => request<ModelState>('/models')

export const switchModel = (name: string) =>
  post<{ status: string; current: ModelInfo }>('/models/active', { name })

export const showEmotion = (label: string) =>
  post<{ label: string; emotion: number }>('/models/emotion', { label })

// ---- TTS ----

export interface TTSEngine {
  name: string
  enabled: boolean
  ready: boolean
  voice?: string
  model?: string
  reason?: string
}

export const getTTS = () => request<{ count: number; engines: TTSEngine[] }>('/tts')

/** 试听返回 WAV 二进制；失败时抛 ApiError（502 合成失败）。 */
export async function previewTTS(text: string, engine?: string, voice?: string): Promise<Blob> {
  const res = await fetch('/api/tts/preview', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, engine: engine ?? '', voice: voice ?? '' }),
  })
  if (!res.ok) {
    const raw = await res.text().catch(() => '')
    let message = `HTTP ${res.status}`
    try {
      const body = JSON.parse(raw) as { error?: string }
      message = body.error ?? raw
    } catch {
      if (raw) message = raw
    }
    throw new ApiError(res.status, message)
  }
  return res.blob()
}

// ---- 推流 ----

export interface StreamInfo {
  enabled: boolean
  running: boolean
  streaming: boolean
  output?: string
  pending_code?: number
  pending_note?: string
  verify_url?: string
}

export const getStream = () => request<StreamInfo>('/stream')

export const streamAction = (action: 'start' | 'stop') =>
  post<{ status: string; stream: StreamInfo }>('/stream', { action })

// ---- 日志 ----

export interface LogEntry {
  time: string
  level: string
  msg: string
}

export const getLogs = (limit: number, level?: string) => {
  const params = new URLSearchParams({ limit: String(limit) })
  if (level) params.set('level', level)
  return request<{ count: number; entries: LogEntry[] }>(`/logs?${params.toString()}`)
}

// ---- 对话归档 ----

export interface ChatChannel {
  channel_id: string
  count: number
  last_time?: string
  last_text?: string
}

export interface ChatPlatform {
  platform: string
  channels: ChatChannel[]
}

export interface ChatRecord {
  kind: 'dialogue' | 'event'
  time: string
  platform: string
  channel_id: string
  channel_type?: string
  user_id?: string
  user_name?: string
  event_kind: string
  text: string
  reply?: string
}

export const getChats = () => request<{ platforms: ChatPlatform[] }>('/chats')

export const getChatRecords = (platform: string, channel: string, limit: number, before?: string) => {
  const params = new URLSearchParams({ limit: String(limit) })
  if (before) params.set('before', before)
  return request<{ platform: string; channel_id: string; count: number; records: ChatRecord[] }>(
    `/chats/${encodeURIComponent(platform)}/${encodeURIComponent(channel)}?${params.toString()}`,
  )
}

/** 导出下载地址；交给浏览器直接打开（带 Content-Disposition）。 */
export function chatExportURL(platform: string, channel: string, format: 'jsonl' | 'md'): string {
  const params = new URLSearchParams({ platform, format })
  if (channel) params.set('channel', channel)
  return `/api/chats/export?${params.toString()}`
}
