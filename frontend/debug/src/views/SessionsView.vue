<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  ApiError,
  getHistory,
  getSessions,
  sendMessage,
  type HistoryMessage,
  type SessionInfo,
} from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { formatTime } from '../lib/format'
import { toast } from '../lib/toast'

const sessions = ref<SessionInfo[]>([])
const disabled = ref(false)
const error = ref('')

const selected = ref<string | null>(null)
const history = ref<HistoryMessage[]>([])
const historyError = ref('')
const sending = ref(false)
const draft = ref('')
const draftUser = ref('调试员')

async function refresh(): Promise<void> {
  try {
    const snapshot = await getSessions()
    sessions.value = snapshot.sessions ?? []
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

async function openChannel(channel: string): Promise<void> {
  selected.value = channel
  history.value = []
  historyError.value = ''
  await loadHistory()
}

async function loadHistory(): Promise<void> {
  if (!selected.value) return
  try {
    const payload = await getHistory(selected.value)
    history.value = payload.messages ?? []
  } catch (err) {
    historyError.value = err instanceof Error ? err.message : String(err)
  }
}

async function send(): Promise<void> {
  const text = draft.value.trim()
  if (!selected.value || !text) return

  sending.value = true
  try {
    await sendMessage(selected.value, { text, user_name: draftUser.value.trim() || '调试员' })
    draft.value = ''
    toast('ok', '已投递（异步生成回复，稍后刷新可见）')
    window.setTimeout(() => void loadHistory(), 2200)
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    sending.value = false
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
  <PanelCard title="渠道会话" hint="GET /api/sessions · 点击行查看历史" :delay="0">
    <template #actions>
      <button class="btn small" @click="refresh">刷新</button>
    </template>

    <EmptyState
      v-if="disabled"
      title="会话未启用"
      hint="未配置 [llm] 的 base_url / model，事件不会被处理"
    />

    <template v-else>
      <div v-if="error" class="err mono">{{ error }}</div>

      <DataTable
        :columns="[
          { key: 'channel_id', label: '渠道', width: '180px' },
          { key: 'pending', label: '待处理', width: '70px' },
          { key: 'history_len', label: '历史条数', width: '80px' },
          { key: 'last_active', label: '最近活跃' },
          { key: 'last_idle', label: '最近主动发言' },
        ]"
        :rows="sessions"
        row-key="channel_id"
        :active-key="selected"
        clickable
        empty="还没有任何渠道会话（发一条消息就会创建）"
        @row="(row) => openChannel(String(row.channel_id))"
      >
        <template #channel_id="{ row }">
          <span class="mono">{{ row.channel_id }}</span>
        </template>
        <template #last_active="{ row }">
          <span class="mono dim">{{ formatTime(row.last_active as string) }}</span>
        </template>
        <template #last_idle="{ row }">
          <span class="mono dim">{{ formatTime(row.last_idle as string) }}</span>
        </template>
      </DataTable>

      <div v-if="selected" class="detail">
        <div class="row">
          <h3 class="mono">{{ selected }}</h3>
          <span class="faint">{{ history.length }} 条历史</span>
          <span class="spacer"></span>
          <button class="btn small" @click="loadHistory">刷新历史</button>
        </div>

        <div v-if="historyError" class="err mono">{{ historyError }}</div>

        <div class="history">
          <div v-if="history.length === 0" class="faint">历史为空</div>
          <div v-for="(message, index) in history" :key="index" class="msg" :class="message.role">
            <span class="role mono">{{ message.role }}</span>
            <span class="content">{{ message.content }}</span>
          </div>
        </div>

        <div class="send row">
          <input v-model="draftUser" type="text" class="user-input" placeholder="用户名" />
          <input
            v-model="draft"
            type="text"
            class="text-input"
            placeholder="以观众身份发一条消息（走真实上传管线）"
            @keyup.enter="send"
          />
          <button class="btn primary" :disabled="sending || !draft.trim()" @click="send">
            {{ sending ? '发送中…' : '发送' }}
          </button>
        </div>
      </div>
    </template>
  </PanelCard>
</template>

<style scoped>
.detail h3 {
  font-size: 14px;
  font-weight: 500;
}
.history {
  margin-top: 10px;
  max-height: 340px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.msg {
  display: grid;
  grid-template-columns: 80px 1fr;
  gap: 10px;
  padding: 5px 8px;
  border-radius: 4px;
  background: var(--bg-raise);
  border: 1px solid var(--edge);
}
.msg .role {
  font-size: 11px;
  color: var(--text-faint);
}
.msg.assistant .role {
  color: var(--accent);
}
.msg.system .role {
  color: var(--signal);
}
.msg.tool .role {
  color: var(--ok);
}
.content {
  word-break: break-word;
  white-space: pre-wrap;
}
.send {
  margin-top: 12px;
}
.user-input {
  width: 120px;
  flex: none;
}
.text-input {
  flex: 1;
}
</style>
