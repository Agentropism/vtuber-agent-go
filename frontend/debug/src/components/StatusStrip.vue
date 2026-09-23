<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError, getStatus, getStream, type StatusSnapshot, type StreamInfo } from '../lib/api'
import { useTheme } from '../lib/theme'
import LedDot from './LedDot.vue'

const { theme, toggleTheme } = useTheme()

const status = ref<StatusSnapshot | null>(null)
const stream = ref<StreamInfo | null>(null)
const streamDisabled = ref(false)

async function refresh(): Promise<void> {
  try {
    status.value = await getStatus()
  } catch {
    // 网关重启的瞬间请求会失败，保留上一次快照即可
  }

  try {
    stream.value = await getStream()
    streamDisabled.value = false
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      streamDisabled.value = true
      stream.value = null
    }
  }
}

let timer = 0
onMounted(() => {
  void refresh()
  timer = window.setInterval(() => void refresh(), 5000)
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <header class="strip">
    <div class="cell">
      <span class="label">运行</span>
      <span class="value mono">{{ status?.uptime ?? '—' }}</span>
    </div>

    <div class="cell">
      <span class="label">接入</span>
      <template v-if="status?.clients">
        <template v-if="status.clients.count > 0">
          <LedDot
            v-for="platform in status.clients.platforms"
            :key="platform"
            state="ok"
            :label="platform"
          />
        </template>
        <span v-else class="faint">无连接</span>
      </template>
      <span v-else class="faint">未启用</span>
    </div>

    <div class="cell">
      <span class="label">播报</span>
      <template v-if="status?.broadcast">
        <span class="value mono">
          {{ status.broadcast.enqueued }} 入队 / {{ status.broadcast.played }} 播完
        </span>
        <span v-if="status.broadcast.dropped + status.broadcast.failed > 0" class="err mono">
          丢弃 {{ status.broadcast.dropped + status.broadcast.failed }}
        </span>
      </template>
      <span v-else class="faint">未启用</span>
    </div>

    <div class="cell">
      <span class="label">上报队列</span>
      <template v-if="status?.upload">
        <span class="value mono">{{ status.upload.queue_len }}/{{ status.upload.queue_size }}</span>
        <span v-if="status.upload.dropped > 0" class="signal mono">丢 {{ status.upload.dropped }}</span>
      </template>
      <span v-else class="faint">—</span>
    </div>

    <div class="cell">
      <span class="label">会话</span>
      <span class="value mono">{{ status?.sessions ?? '—' }}</span>
    </div>

    <div class="spacer"></div>

    <!-- 主题切换：按钮显示「将要切到」的形态 -->
    <button
      class="theme-toggle"
      :title="theme === 'dark' ? '切换到白天模式' : '切换到黑夜模式'"
      @click="toggleTheme"
    >
      <svg v-if="theme === 'dark'" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <svg v-else viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
      <span class="theme-label">{{ theme === 'dark' ? '白天' : '黑夜' }}</span>
    </button>

    <!-- 导播 tally 灯：推流中亮红 -->
    <div class="tally">
      <template v-if="streamDisabled">
        <LedDot state="idle" label="推流未启用" />
      </template>
      <template v-else-if="stream?.streaming">
        <LedDot state="on" label="ON AIR" />
      </template>
      <template v-else>
        <LedDot state="warn" :label="stream?.running ? '推流就绪' : '待机'" />
      </template>
    </div>
  </header>
</template>

<style scoped>
.strip {
  height: var(--strip-h);
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 0 18px;
  border-bottom: 1px solid var(--edge);
  background: var(--bg-raise);
  overflow-x: auto;
  white-space: nowrap;
}
.cell {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.label {
  font-family: var(--font-display);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-faint);
}
.value {
  font-size: 12.5px;
}
.tally {
  padding-left: 14px;
  border-left: 1px solid var(--edge);
}
.theme-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  border: 1px solid var(--edge-strong);
  border-radius: 4px;
  padding: 3px 9px;
  color: var(--text-dim);
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease;
}
.theme-toggle:hover {
  border-color: var(--accent);
  color: var(--accent);
}
.theme-label {
  font-size: 12px;
}
</style>
