// 主题（黑夜/白天）：默认跟随系统，手动切换后记住选择。
//
// 主题通过 <html data-theme="..."> 表达，样式在 styles/tokens.css 里按
// :root（黑夜）与 html[data-theme='light']（白天）两套令牌定义。
import { ref } from 'vue'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'vtuber-debug-theme'

const theme = ref<Theme>('dark')

function apply(next: Theme): void {
  theme.value = next
  document.documentElement.dataset.theme = next
}

/** 初始主题：URL 参数（临时，不持久化）> 用户上次的选择 > 跟随系统。 */
export function initTheme(): void {
  // ?theme=light / ?theme=dark：方便分享链接与截图，不改用户已保存的偏好
  const fromURL = new URLSearchParams(window.location.search).get('theme')
  if (fromURL === 'dark' || fromURL === 'light') {
    apply(fromURL)
    return
  }

  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === 'dark' || saved === 'light') {
    apply(saved)
    return
  }

  apply(window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark')
}

/** 切换主题并记住选择。 */
export function toggleTheme(): void {
  const next: Theme = theme.value === 'dark' ? 'light' : 'dark'
  localStorage.setItem(STORAGE_KEY, next)
  apply(next)
}

export function useTheme(): { theme: typeof theme; toggleTheme: typeof toggleTheme } {
  return { theme, toggleTheme }
}
