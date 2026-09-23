// 极简 Toast：全局响应式队列 + 自动消失，ToastHost 组件负责渲染。
import { reactive } from 'vue'

export type ToastKind = 'ok' | 'err' | 'info'

export interface ToastItem {
  id: number
  kind: ToastKind
  text: string
}

const state = reactive<{ items: ToastItem[] }>({ items: [] })
let seq = 0

export function toast(kind: ToastKind, text: string): void {
  const item: ToastItem = { id: ++seq, kind, text }
  state.items.push(item)

  window.setTimeout(() => {
    const index = state.items.findIndex((it) => it.id === item.id)
    if (index >= 0) state.items.splice(index, 1)
  }, 3600)
}

export function useToasts(): { items: ToastItem[] } {
  return state
}
