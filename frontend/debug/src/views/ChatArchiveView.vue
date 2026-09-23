<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  ApiError,
  chatExportURL,
  getChatRecords,
  getChats,
  type ChatPlatform,
  type ChatRecord,
} from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { formatDateTime, formatTime, kindLabel } from '../lib/format'
import { toast } from '../lib/toast'

const platforms = ref<ChatPlatform[]>([])
const disabled = ref(false)
const error = ref('')

const selected = ref<{ platform: string; channel: string } | null>(null)
const records = ref<ChatRecord[]>([])
const loading = ref(false)
const hasMore = ref(false)

async function load(): Promise<void> {
  try {
    const payload = await getChats()
    platforms.value = payload.platforms ?? []
    disabled.value = false
    error.value = ''
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      disabled.value = true
      return
    }
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function openChannel(platform: string, channel: string): Promise<void> {
  selected.value = { platform, channel }
  records.value = []
  hasMore.value = false
  await loadRecords()
}

async function loadRecords(before?: string): Promise<void> {
  if (!selected.value) return

  loading.value = true
  try {
    const payload = await getChatRecords(selected.value.platform, selected.value.channel, 100, before)
    const incoming = payload.records ?? []
    records.value = before ? [...records.value, ...incoming] : incoming
    hasMore.value = incoming.length >= 100
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    loading.value = false
  }
}

function loadMore(): void {
  const last = records.value[records.value.length - 1]
  if (last) void loadRecords(last.time)
}

function exportArchive(channel: string, format: 'jsonl' | 'md'): void {
  if (!selected.value) return
  window.open(chatExportURL(selected.value.platform, channel, format), '_blank')
}

const route = useRoute()

onMounted(async () => {
  await load()

  // 深链：#/chats?platform=local&channel=room_local 直接打开某渠道
  const platform = String(route.query.platform ?? '')
  const channel = String(route.query.channel ?? '')
  if (platform && channel) {
    await openChannel(platform, channel)
  }
})
</script>

<template>
  <div class="layout">
    <EmptyState
      v-if="disabled"
      title="对话归档未启用"
      hint="未配置 [agent].archive_dir（data/chats），归档写入与恢复都关闭"
    />

    <template v-else>
      <PanelCard title="归档分类" hint="只增不删 · 按平台/渠道分类" :delay="0">
        <template #actions>
          <button class="btn small" @click="load">刷新</button>
        </template>

        <div v-if="error" class="err mono">{{ error }}</div>

        <div v-if="platforms.length === 0" class="faint">还没有归档记录（聊一句就会写入）</div>

        <div v-for="platform in platforms" :key="platform.platform" class="platform">
          <div class="platform-head">
            <span class="platform-name mono">{{ platform.platform }}</span>
            <button class="btn small" @click="exportArchive('', 'jsonl')">导出全部</button>
          </div>

          <button
            v-for="channel in platform.channels"
            :key="channel.channel_id"
            class="channel"
            :class="{
              active:
                selected?.platform === platform.platform && selected?.channel === channel.channel_id,
            }"
            @click="openChannel(platform.platform, channel.channel_id)"
          >
            <span class="channel-id mono">{{ channel.channel_id }}</span>
            <span class="badge">{{ channel.count }}</span>
            <span class="channel-last dim">{{ channel.last_text }}</span>
            <span class="channel-time mono faint">{{ formatTime(channel.last_time) }}</span>
          </button>
        </div>
      </PanelCard>

      <PanelCard
        :title="selected ? `记录 · ${selected.platform}/${selected.channel}` : '记录'"
        hint="新的在前 · 每页 100 条"
        :delay="60"
      >
        <template #actions>
          <template v-if="selected">
            <button class="btn small" @click="exportArchive(selected.channel, 'jsonl')">jsonl</button>
            <button class="btn small" @click="exportArchive(selected.channel, 'md')">markdown</button>
          </template>
        </template>

        <div v-if="!selected" class="faint">左侧选择一个渠道</div>

        <template v-else>
          <DataTable
            :columns="[
              { key: 'kind', label: '类型', width: '64px' },
              { key: 'time', label: '时间', width: '150px' },
              { key: 'user', label: '用户', width: '110px' },
              { key: 'event_kind', label: '事件', width: '86px' },
              { key: 'text', label: '内容' },
              { key: 'reply', label: '回复' },
            ]"
            :rows="records as unknown as Record<string, unknown>[]"
            empty="没有记录"
          >
            <template #kind="{ row }">
              <span class="badge" :class="row.kind === 'dialogue' ? 'accent' : ''">
                {{ row.kind === 'dialogue' ? '对话' : '事件' }}
              </span>
            </template>
            <template #time="{ row }">
              <span class="mono dim">{{ formatDateTime(row.time as string) }}</span>
            </template>
            <template #user="{ row }">
              <span>{{ row.user_name || row.user_id || '—' }}</span>
            </template>
            <template #event_kind="{ row }">
              <span class="dim">{{ kindLabel(String(row.event_kind ?? '')) }}</span>
            </template>
            <template #text="{ row }">
              <span class="break">{{ row.text }}</span>
            </template>
            <template #reply="{ row }">
              <span v-if="row.reply" class="break reply">{{ row.reply }}</span>
              <span v-else class="faint">—</span>
            </template>
          </DataTable>

          <div v-if="hasMore" class="row more">
            <span class="spacer"></span>
            <button class="btn small" :disabled="loading" @click="loadMore">
              {{ loading ? '加载中…' : '加载更早的记录' }}
            </button>
          </div>
        </template>
      </PanelCard>
    </template>
  </div>
</template>

<style scoped>
.layout {
  display: grid;
  grid-template-columns: 340px 1fr;
  gap: var(--gap);
  align-items: start;
}
@media (max-width: 1100px) {
  .layout {
    grid-template-columns: 1fr;
  }
}
.platform {
  margin-bottom: 14px;
}
.platform-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.platform-name {
  color: var(--accent);
  font-size: 13px;
  letter-spacing: 0.06em;
}
.channel {
  display: grid;
  grid-template-columns: 1fr auto;
  grid-template-rows: auto auto;
  gap: 0 8px;
  width: 100%;
  text-align: left;
  background: var(--bg-raise);
  border: 1px solid var(--edge);
  border-radius: 4px;
  padding: 6px 9px;
  margin-bottom: 4px;
  cursor: pointer;
  color: var(--text);
}
.channel:hover {
  border-color: var(--accent);
}
.channel.active {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.channel-id {
  font-size: 12.5px;
}
.channel-last {
  grid-column: 1 / -1;
  font-size: 11.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 300px;
}
.channel-time {
  font-size: 11px;
  text-align: right;
}
.reply {
  color: var(--text-dim);
}
.more {
  margin-top: 10px;
}
</style>
