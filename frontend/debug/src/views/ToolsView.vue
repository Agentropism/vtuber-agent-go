<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, callTool, getTools, type ToolInfo } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { prettyJSON } from '../lib/format'
import { toast } from '../lib/toast'

const tools = ref<ToolInfo[]>([])
const disabled = ref(false)
const error = ref('')

const selected = ref('')
const argsText = ref('{}')
const result = ref('')
const calling = ref(false)

async function load(): Promise<void> {
  try {
    const payload = await getTools()
    tools.value = payload.tools ?? []
    disabled.value = false
    error.value = ''
    if (!selected.value && tools.value.length > 0) {
      const first = tools.value.find((tool) => tool.read_only) ?? tools.value[0]
      selected.value = first.name
    }
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      disabled.value = true
      return
    }
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function invoke(): Promise<void> {
  if (!selected.value) return

  let args: unknown
  try {
    args = argsText.value.trim() ? JSON.parse(argsText.value) : {}
  } catch {
    toast('err', '参数不是合法 JSON')
    return
  }

  calling.value = true
  result.value = ''
  try {
    const payload = await callTool(selected.value, args)
    result.value = prettyJSON(payload.result)
    toast('ok', `工具 ${payload.name} 执行完成`)
  } catch (err) {
    result.value = err instanceof Error ? err.message : String(err)
    toast('err', '调用失败')
  } finally {
    calling.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="stack">
    <EmptyState
      v-if="disabled"
      title="工具未启用"
      hint="未开启 [agent].enable_tools（记忆检索 memory_search / 状态查询 bot_status）"
    />

    <template v-else>
      <PanelCard title="已注册工具" hint="GET /api/tools" :delay="0">
        <template #actions>
          <button class="btn small" @click="load">刷新</button>
        </template>
        <div v-if="error" class="err mono">{{ error }}</div>

        <DataTable
          :columns="[
            { key: 'name', label: '工具', width: '150px' },
            { key: 'read_only', label: '只读', width: '70px' },
            { key: 'description', label: '说明' },
            { key: 'parameters', label: '参数定义' },
          ]"
          :rows="tools"
          empty="没有注册任何工具"
        >
          <template #name="{ row }">
            <span class="mono">{{ row.name }}</span>
          </template>
          <template #read_only="{ row }">
            <span class="badge" :class="row.read_only ? 'ok' : 'warn'">
              {{ row.read_only ? '可直调' : '有副作用' }}
            </span>
          </template>
          <template #parameters="{ row }">
            <details v-if="row.parameters">
              <summary class="faint">展开</summary>
              <pre class="json">{{ prettyJSON(row.parameters) }}</pre>
            </details>
            <span v-else class="faint">—</span>
          </template>
        </DataTable>
      </PanelCard>

      <PanelCard
        title="直接调用"
        hint="POST /api/tools/{name} · 只放行只读工具（403 表示有副作用）"
        :delay="60"
      >
        <div class="row wrap">
          <select v-model="selected">
            <option v-for="tool in tools" :key="tool.name" :value="tool.name">
              {{ tool.name }}{{ tool.read_only ? '' : '（有副作用）' }}
            </option>
          </select>
          <span class="faint">参数（JSON，空对象即可）</span>
        </div>
        <textarea v-model="argsText" rows="3" class="args"></textarea>
        <div class="row">
          <span class="spacer"></span>
          <button class="btn primary" :disabled="calling || !selected" @click="invoke">
            {{ calling ? '执行中…' : '执行' }}
          </button>
        </div>
        <pre v-if="result" class="json result">{{ result }}</pre>
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
.args {
  width: 100%;
  margin: 8px 0;
}
.json {
  margin: 6px 0 0;
  padding: 8px 10px;
  background: var(--bg-raise);
  border: 1px solid var(--edge);
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 12px;
  max-height: 260px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.json.result {
  border-left: 2px solid var(--accent);
}
</style>
