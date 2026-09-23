<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, getModels, showEmotion, switchModel, type ModelState } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { toast } from '../lib/toast'

const state = ref<ModelState | null>(null)
const disabled = ref(false)
const error = ref('')
const switching = ref(false)
const lastEmotion = ref('')

async function load(): Promise<void> {
  try {
    state.value = await getModels()
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

async function activate(name: string): Promise<void> {
  switching.value = true
  try {
    await switchModel(name)
    toast('ok', `已切换模型：${name}（舞台页会收到 hello 并换模）`)
    await load()
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    switching.value = false
  }
}

async function previewEmotion(label: string): Promise<void> {
  try {
    const result = await showEmotion(label)
    lastEmotion.value = label
    toast('ok', `表情 [${result.label}] → 表达式下标 ${result.emotion}`)
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
      title="前端未启用"
      hint="未配置 [frontend] 的 models_dir / model_dict，没有 Live2D 能力"
    />

    <template v-else>
      <PanelCard title="当前模型" hint="GET /api/models" :delay="0">
        <template #actions>
          <button class="btn small" @click="load">刷新</button>
        </template>
        <div v-if="error" class="err mono">{{ error }}</div>

        <div v-if="state" class="stat-grid">
          <div class="stat">
            <div class="stat-label">模型名</div>
            <div class="stat-value accent">{{ state.current.name }}</div>
          </div>
          <div class="stat">
            <div class="stat-label">缩放</div>
            <div class="stat-value">{{ state.current.scale }}</div>
          </div>
          <div class="stat">
            <div class="stat-label">偏移 X / Y</div>
            <div class="stat-value">{{ state.current.x_shift ?? 0 }} / {{ state.current.y_shift ?? 0 }}</div>
          </div>
          <div class="stat">
            <div class="stat-label">待机动作</div>
            <div class="stat-value mono">{{ state.current.idle_motion || '—' }}</div>
          </div>
          <div class="stat">
            <div class="stat-label">表达式数</div>
            <div class="stat-value">{{ state.current.expressions?.length ?? 0 }}</div>
          </div>
        </div>
      </PanelCard>

      <PanelCard title="可用模型" hint="点击切换（会向前端补发 hello）" :delay="60">
        <DataTable
          v-if="state"
          :columns="[
            { key: 'name', label: '模型', width: '160px' },
            { key: 'url', label: '路径' },
            { key: 'scale', label: '缩放', width: '70px' },
            { key: 'actions', label: '', width: '90px' },
          ]"
          :rows="state.models"
          empty="模型目录下没有可用模型"
        >
          <template #name="{ row }">
            <span class="mono">{{ row.name }}</span>
            <span v-if="row.name === state.current.name" class="badge accent current">当前</span>
          </template>
          <template #url="{ row }">
            <span class="mono dim break">{{ row.url }}</span>
          </template>
          <template #actions="{ row }">
            <button
              class="btn small"
              :disabled="switching || row.name === state.current.name"
              @click="activate(String(row.name))"
            >
              切换
            </button>
          </template>
        </DataTable>
      </PanelCard>

      <PanelCard title="表情预览" hint="POST /api/models/emotion · 直接推给舞台页" :delay="120">
        <div v-if="state && state.emotions.length" class="emotions">
          <button
            v-for="label in state.emotions"
            :key="label"
            class="btn small emotion"
            :class="{ active: label === lastEmotion }"
            @click="previewEmotion(label)"
          >
            {{ label }}
          </button>
        </div>
        <div v-else class="faint">当前模型没有表情标签</div>
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
.current {
  margin-left: 8px;
}
.emotions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.emotion.active {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-soft);
}
</style>
