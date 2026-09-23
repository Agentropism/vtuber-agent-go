// 展示格式化工具。

/** 把 RFC3339 时间显示成 MM-DD HH:mm:ss；空值给占位符。 */
export function formatTime(value?: string | null): string {
  if (!value) return '—'
  // Go 的零值时间（0001-01-01）视为「无」
  if (value.startsWith('0001-01-01')) return '—'

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(
    date.getMinutes(),
  )}:${pad(date.getSeconds())}`
}

/** 完整日期时间（记录详情用）。 */
export function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/** 大数字加千分位。 */
export function formatNumber(value: number | undefined | null): string {
  if (value === undefined || value === null) return '—'
  return value.toLocaleString('en-US')
}

/** 事件种类 → 中文标签（未知种类原样显示）。 */
export function kindLabel(kind: string): string {
  const map: Record<string, string> = {
    group_message: '群消息',
    danmaku: '弹幕',
    super_chat: '醒目留言',
    gift: '礼物',
    guard: '大航海',
    notice: '通知',
    like: '点赞',
    live_room_enter: '进房',
    live_start: '开播',
    live_end: '下播',
  }
  return map[kind] ?? kind
}

/** 把对象格式化成带缩进的 JSON；失败时退回 String()。 */
export function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/** 触发一次浏览器下载（试听音频之外的文本类导出用）。 */
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
