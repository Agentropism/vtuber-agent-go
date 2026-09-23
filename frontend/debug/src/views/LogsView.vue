<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { getLogs, type LogEntry } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import LogViewer from '../components/LogViewer.vue'
import { toast } from '../lib/toast'

const entries = ref<LogEntry[]>([])
const level = ref('')
const limit = ref(200)
const auto = ref(true)
const loading = ref(false)

const levels = [
  { value: '', label: '全部' },
  { value: 'debug', label: 'DEBUG' },
  { value: 'info', label: 'INFO' },
  { value: 'warn', label: 'WARN' },
  { value: 'error', label: 'ERROR' },
]

async function load(): Promise<void> {
  loading.value = true
  try {
    const payload = await getLogs(limit.value, level.value || undefined)
    entries.value = payload.entries ?? []
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

let timer = 0
function restartTimer(): void {
  window.clearInterval(timer)
  if (auto.value) {
    timer = window.setInterval(() => void load(), 2000)
  }
}

onMounted(() => {
  void load()
  restartTimer()
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <PanelCard
    title="进程日志"
    hint="GET /api/logs · 环形缓冲最近 500 条（新的在前）"
    :delay="0"
  >
    <template #actions>
      <span class="faint">显示 {{ entries.length }} 条</span>
    </template>

    <div class="controls row wrap">
      <div class="levels">
        <button
          v-for="item in levels"
          :key="item.value"
          class="btn small"
          :class="{ primary: level === item.value }"
          @click="((level = item.value), load())"
        >
          {{ item.label }}
        </button>
      </div>

      <select v-model.number="limit" @change="load">
        <option :value="100">100 条</option>
        <option :value="200">200 条</option>
        <option :value="500">500 条</option>
      </select>

      <label class="auto">
        <input v-model="auto" type="checkbox" @change="restartTimer" />
        <span class="dim">自动刷新（2s）</span>
      </label>

      <span class="spacer"></span>
      <button class="btn small" :disabled="loading" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <LogViewer :entries="entries" />
  </PanelCard>
</template>

<style scoped>
.controls {
  margin-bottom: 10px;
}
.levels {
  display: flex;
  gap: 4px;
}
.auto {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
</style>
