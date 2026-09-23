<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError, getStream, streamAction, type StreamInfo } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import EmptyState from '../components/EmptyState.vue'
import LedDot from '../components/LedDot.vue'
import { toast } from '../lib/toast'

const info = ref<StreamInfo | null>(null)
const disabled = ref(false)
const error = ref('')
const busy = ref(false)

async function load(): Promise<void> {
  try {
    info.value = await getStream()
    disabled.value = false
    error.value = ''
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      disabled.value = true
      info.value = null
      return
    }
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function action(kind: 'start' | 'stop'): Promise<void> {
  const label = kind === 'start' ? '开始推流' : '停止推流'
  if (!window.confirm(`确定要${label}吗？`)) return

  busy.value = true
  try {
    const payload = await streamAction(kind)
    info.value = payload.stream
    toast('ok', `${label}已提交`)
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    busy.value = false
    await load()
  }
}

let timer = 0
onMounted(() => {
  void load()
  timer = window.setInterval(() => void load(), 3000)
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <div class="stack">
    <EmptyState
      v-if="disabled"
      title="推流未启用"
      hint="未启用 [stream].enabled，网关只做对话与播报"
    />

    <PanelCard v-else title="推流状态" hint="GET /api/stream · 每 3 秒刷新" :delay="0">
      <template #actions>
        <LedDot :state="info?.streaming ? 'on' : info?.running ? 'warn' : 'idle'" />
        <span class="badge" :class="info?.streaming ? 'err' : info?.running ? 'warn' : ''">
          {{ info?.streaming ? 'ON AIR' : info?.running ? '待推流' : '待机' }}
        </span>
      </template>

      <div v-if="error" class="err mono">{{ error }}</div>

      <div v-if="info" class="stat-grid">
        <div class="stat">
          <div class="stat-label">循环</div>
          <div class="stat-value">{{ info.running ? '运行中' : '已停止' }}</div>
        </div>
        <div class="stat">
          <div class="stat-label">推流</div>
          <div class="stat-value" :class="info.streaming ? 'err' : ''">
            {{ info.streaming ? '推流中' : '未推流' }}
          </div>
        </div>
      </div>

      <dl v-if="info" class="kv detail">
        <dt>输出地址</dt>
        <dd>{{ info.output || '—（未取到推流地址）' }}</dd>
        <template v-if="info.pending_code || info.pending_note">
          <dt>开播受阻</dt>
          <dd class="signal">
            {{ info.pending_code ? `[${info.pending_code}] ` : '' }}{{ info.pending_note }}
          </dd>
        </template>
        <template v-if="info.verify_url">
          <dt>验证页</dt>
          <dd>
            <a :href="info.verify_url" target="_blank" rel="noreferrer">{{ info.verify_url }}</a>
          </dd>
        </template>
      </dl>

      <div class="row actions">
        <button class="btn primary" :disabled="busy || !info || info.streaming" @click="action('start')">
          {{ busy ? '处理中…' : '开始推流' }}
        </button>
        <button class="btn danger" :disabled="busy || !info || (!info.running && !info.streaming)" @click="action('stop')">
          停止推流
        </button>
        <span class="faint">停止最多等待 10 秒（等关播收尾）</span>
      </div>
    </PanelCard>
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: var(--gap);
}
.actions {
  margin-top: 14px;
}
</style>
