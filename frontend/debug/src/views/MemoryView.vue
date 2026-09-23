<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, addMemory, deleteMemory, getMemory, type MemoryEntry } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { formatTime } from '../lib/format'
import { toast } from '../lib/toast'

const entries = ref<MemoryEntry[]>([])
const total = ref(0)
const disabled = ref(false)
const error = ref('')

const query = ref('')
const limit = ref(20)

const draft = ref({ channel_id: '', user: '', text: '', reply: '', weight: 1 })

async function load(): Promise<void> {
  try {
    const payload = await getMemory(query.value.trim(), limit.value)
    entries.value = payload.entries ?? []
    total.value = payload.total
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

async function submit(): Promise<void> {
  const text = draft.value.text.trim()
  if (!text) {
    toast('info', '文本不能为空')
    return
  }

  try {
    const result = await addMemory({
      channel_id: draft.value.channel_id.trim(),
      user: draft.value.user.trim(),
      text,
      reply: draft.value.reply.trim(),
      weight: Number(draft.value.weight) || 0,
    })
    toast('ok', `已写入记忆 #${result.id}`)
    draft.value = { channel_id: '', user: '', text: '', reply: '', weight: 1 }
    await load()
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  }
}

async function remove(entry: MemoryEntry): Promise<void> {
  if (!window.confirm(`删除记忆 #${entry.id}？会写入一条墓碑标记。`)) return

  try {
    await deleteMemory(entry.id)
    toast('ok', `已删除 #${entry.id}`)
    await load()
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  }
}

onMounted(load)
</script>

<template>
  <div class="stack">
    <EmptyState
      v-if="disabled"
      title="记忆未启用"
      hint="未配置 [agent].memory_file（data/memory.jsonl）"
    />

    <template v-else>
      <PanelCard title="记忆检索" hint="GET /api/memory · 带 query 为关键词召回，不带为最近记录" :delay="0">
        <template #actions>
          <span class="faint">共 {{ total }} 条</span>
        </template>

        <div class="row wrap">
          <input
            v-model="query"
            type="text"
            class="query"
            placeholder="关键词（中文按二元组召回）"
            @keyup.enter="load"
          />
          <select v-model.number="limit">
            <option :value="10">10 条</option>
            <option :value="20">20 条</option>
            <option :value="50">50 条</option>
          </select>
          <button class="btn primary" @click="load">检索</button>
          <button class="btn" @click="((query = ''), load())">重置</button>
        </div>

        <div v-if="error" class="err mono">{{ error }}</div>

        <DataTable
          :columns="[
            { key: 'id', label: 'ID', width: '56px' },
            { key: 'time', label: '时间', width: '120px' },
            { key: 'channel_id', label: '渠道', width: '130px' },
            { key: 'user', label: '用户', width: '100px' },
            { key: 'text', label: '用户说' },
            { key: 'reply', label: '回复' },
            { key: 'weight', label: '权重', width: '56px' },
            { key: 'actions', label: '', width: '70px' },
          ]"
          :rows="entries"
          empty="没有匹配的记忆"
        >
          <template #id="{ row }">
            <span class="mono">#{{ row.id }}</span>
          </template>
          <template #time="{ row }">
            <span class="mono dim">{{ formatTime(row.time as string) }}</span>
          </template>
          <template #channel_id="{ row }">
            <span class="mono dim">{{ row.channel_id || '—' }}</span>
          </template>
          <template #weight="{ row }">
            <span class="mono" :class="Number(row.weight) >= 3 ? 'signal' : ''">{{ row.weight }}</span>
          </template>
          <template #actions="{ row }">
            <button class="btn small danger" @click="remove(row as unknown as MemoryEntry)">删除</button>
          </template>
        </DataTable>
      </PanelCard>

      <PanelCard title="写入记忆" hint="POST /api/memory · 权重：普通 1，礼物 3，SC 5" :delay="60">
        <div class="form-grid">
          <label>
            <span class="faint">渠道（可空）</span>
            <input v-model="draft.channel_id" type="text" placeholder="room_local" />
          </label>
          <label>
            <span class="faint">用户（可空）</span>
            <input v-model="draft.user" type="text" placeholder="调试员" />
          </label>
          <label>
            <span class="faint">权重</span>
            <input v-model.number="draft.weight" type="number" min="0" max="9" />
          </label>
        </div>
        <div class="form-grid wide">
          <label>
            <span class="faint">用户说（必填）</span>
            <input v-model="draft.text" type="text" placeholder="我喜欢吃拉面" @keyup.enter="submit" />
          </label>
          <label>
            <span class="faint">当时的回复（可空）</span>
            <input v-model="draft.reply" type="text" placeholder="那我记住了" @keyup.enter="submit" />
          </label>
        </div>
        <div class="row">
          <span class="spacer"></span>
          <button class="btn primary" :disabled="!draft.text.trim()" @click="submit">写入</button>
        </div>
      </PanelCard>
    </template>
  </div>
</template>

<style scoped>
.stack {
  display: flex;
  flex-direction: column;
  gap: var(--gap);
}
.query {
  width: 260px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(140px, 220px));
  gap: 10px;
  margin-bottom: 10px;
}
.form-grid.wide {
  grid-template-columns: 1fr 1fr;
}
.form-grid label {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.form-grid input {
  width: 100%;
}
</style>
