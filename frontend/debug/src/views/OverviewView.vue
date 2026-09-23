<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { getStatus, type StatusSnapshot } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import Sparkline from '../components/Sparkline.vue'
import LedDot from '../components/LedDot.vue'
import { formatNumber } from '../lib/format'

const status = ref<StatusSnapshot | null>(null)
const error = ref('')

// 播报/上报的增量序列（累计值取差分才是「电平波形」）
const enqueuedSeries = ref<number[]>([])
const playedSeries = ref<number[]>([])
const queueSeries = ref<number[]>([])

let lastEnqueued = 0
let lastPlayed = 0

async function refresh(): Promise<void> {
  try {
    const snapshot = await getStatus()
    status.value = snapshot
    error.value = ''

    if (snapshot.broadcast) {
      enqueuedSeries.value = [
        ...enqueuedSeries.value,
        Math.max(0, snapshot.broadcast.enqueued - lastEnqueued),
      ].slice(-40)
      playedSeries.value = [
        ...playedSeries.value,
        Math.max(0, snapshot.broadcast.played - lastPlayed),
      ].slice(-40)
      lastEnqueued = snapshot.broadcast.enqueued
      lastPlayed = snapshot.broadcast.played
    }
    if (snapshot.upload) {
      queueSeries.value = [...queueSeries.value, snapshot.upload.queue_len].slice(-40)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

let timer = 0
onMounted(() => {
  void refresh()
  timer = window.setInterval(() => void refresh(), 3000)
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <div class="stack">
    <PanelCard title="运行状态" hint="GET /api/status · 每 3 秒刷新" :delay="0">
      <template #actions>
        <button class="btn small" @click="refresh">立即刷新</button>
      </template>

      <div v-if="error" class="err mono">{{ error }}</div>

      <div class="stat-grid">
        <div class="stat">
          <div class="stat-label">运行时长</div>
          <div class="stat-value accent">{{ status?.uptime ?? '—' }}</div>
          <div class="stat-sub mono">{{ status?.started_at ?? '' }}</div>
        </div>

        <div class="stat">
          <div class="stat-label">在线接入</div>
          <div class="stat-value">
            <template v-if="status?.clients">
              <span v-if="status.clients.count === 0" class="faint">无</span>
              <span v-for="platform in status.clients.platforms" :key="platform" class="platform">
                <LedDot state="ok" :label="platform" />
              </span>
            </template>
            <span v-else class="faint">未启用</span>
          </div>
        </div>

        <div class="stat">
          <div class="stat-label">会话数</div>
          <div class="stat-value">{{ status?.sessions ?? '—' }}</div>
        </div>

        <div class="stat">
          <div class="stat-label">上报队列</div>
          <div class="stat-value">
            <template v-if="status?.upload">
              {{ status.upload.queue_len }}<span class="faint">/{{ status.upload.queue_size }}</span>
            </template>
            <span v-else class="faint">—</span>
          </div>
          <div v-if="status?.upload" class="stat-sub">
            累计丢弃 {{ formatNumber(status.upload.dropped) }}
          </div>
        </div>
      </div>
    </PanelCard>

    <div class="two-col">
      <PanelCard title="播报统计" hint="入队 / 播完（累计）" :delay="60">
        <template v-if="status?.broadcast">
          <div class="stat-grid compact">
            <div class="stat">
              <div class="stat-label">入队</div>
              <div class="stat-value">{{ formatNumber(status.broadcast.enqueued) }}</div>
            </div>
            <div class="stat">
              <div class="stat-label">播完</div>
              <div class="stat-value ok">{{ formatNumber(status.broadcast.played) }}</div>
            </div>
            <div class="stat">
              <div class="stat-label">抢占</div>
              <div class="stat-value signal">{{ formatNumber(status.broadcast.preempted) }}</div>
            </div>
            <div class="stat">
              <div class="stat-label">丢弃</div>
              <div class="stat-value" :class="status.broadcast.dropped > 0 ? 'err' : ''">
                {{ formatNumber(status.broadcast.dropped) }}
              </div>
            </div>
            <div class="stat">
              <div class="stat-label">失败</div>
              <div class="stat-value" :class="status.broadcast.failed > 0 ? 'err' : ''">
                {{ formatNumber(status.broadcast.failed) }}
              </div>
            </div>
          </div>

          <div class="wave">
            <div class="wave-label">
              <span class="dim">入队节奏</span>
              <Sparkline :values="enqueuedSeries" :width="220" :height="34" />
            </div>
            <div class="wave-label">
              <span class="dim">播完节奏</span>
              <Sparkline :values="playedSeries" :width="220" :height="34" color="var(--ok)" />
            </div>
          </div>
        </template>
        <div v-else class="faint">未启用播报（未配置 [tts].engines）</div>
      </PanelCard>

      <PanelCard title="上报管线" hint="队列水位（最近 40 次采样）" :delay="120">
        <template v-if="status?.upload">
          <div class="stat-grid compact">
            <div class="stat">
              <div class="stat-label">队列</div>
              <div class="stat-value">{{ status.upload.queue_len }}/{{ status.upload.queue_size }}</div>
            </div>
            <div class="stat">
              <div class="stat-label">累计丢弃</div>
              <div class="stat-value" :class="status.upload.dropped > 0 ? 'err' : ''">
                {{ formatNumber(status.upload.dropped) }}
              </div>
            </div>
          </div>
          <div class="wave">
            <Sparkline :values="queueSeries" :width="460" :height="40" color="var(--signal)" />
          </div>
        </template>
        <div v-else class="faint">—</div>
      </PanelCard>
    </div>
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: var(--gap);
}
.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--gap);
}
@media (max-width: 1100px) {
  .two-col {
    grid-template-columns: 1fr;
  }
}
.stat-grid.compact {
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
}
.platform {
  margin-right: 10px;
}
.wave {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.wave-label {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
}
</style>
