<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  ApiError,
  getModels,
  getTTS,
  previewTTS,
  speak,
  type TTSEngine,
} from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import { toast } from '../lib/toast'

// ---- 播报注入 ----
const speakText = ref('大家好，这里是导播中控台的播报测试。')
const speakEmotion = ref('')
const emotions = ref<string[]>([])
const speaking = ref(false)

async function loadEmotions(): Promise<void> {
  try {
    const state = await getModels()
    emotions.value = state.emotions ?? []
  } catch {
    emotions.value = []
  }
}

async function submitSpeak(): Promise<void> {
  const text = speakText.value.trim()
  if (!text) return

  speaking.value = true
  try {
    await speak(text, speakEmotion.value || undefined)
    toast('ok', '已入播报队列')
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      toast('info', '播报未启用（未配置 [tts].engines）')
    } else {
      toast('err', err instanceof Error ? err.message : String(err))
    }
  } finally {
    speaking.value = false
  }
}

// ---- TTS 引擎 ----
const engines = ref<TTSEngine[]>([])
const ttsDisabled = ref(false)

async function loadEngines(): Promise<void> {
  try {
    const payload = await getTTS()
    engines.value = payload.engines ?? []
    ttsDisabled.value = false
  } catch (err) {
    if (err instanceof ApiError && err.disabled) {
      ttsDisabled.value = true
      engines.value = []
    }
  }
}

// ---- 试听 ----
const previewText = ref('你好，这是一段语音试听。')
const previewEngine = ref('')
const previewVoice = ref('')
const previewing = ref(false)
const audioURL = ref('')

async function submitPreview(): Promise<void> {
  const text = previewText.value.trim()
  if (!text) return

  previewing.value = true
  try {
    const blob = await previewTTS(text, previewEngine.value || undefined, previewVoice.value || undefined)
    if (audioURL.value) URL.revokeObjectURL(audioURL.value)
    audioURL.value = URL.createObjectURL(blob)
    toast('ok', '合成完成')
  } catch (err) {
    toast('err', err instanceof Error ? err.message : String(err))
  } finally {
    previewing.value = false
  }
}

onMounted(() => {
  void loadEmotions()
  void loadEngines()
})
onBeforeUnmount(() => {
  if (audioURL.value) URL.revokeObjectURL(audioURL.value)
})
</script>

<template>
  <div class="stack">
    <PanelCard title="播报注入" hint="POST /api/speak · 入队即返回，合成在后台" :delay="0">
      <div class="form">
        <textarea v-model="speakText" rows="3" placeholder="要播报的文本"></textarea>
        <div class="row">
          <select v-model="speakEmotion">
            <option value="">不指定表情</option>
            <option v-for="label in emotions" :key="label" :value="label">{{ label }}</option>
          </select>
          <span class="faint">表情标签来自当前模型词表</span>
          <span class="spacer"></span>
          <button class="btn primary" :disabled="speaking || !speakText.trim()" @click="submitSpeak">
            {{ speaking ? '入队中…' : '加入播报队列' }}
          </button>
        </div>
      </div>
    </PanelCard>

    <PanelCard title="TTS 引擎" hint="GET /api/tts · 按配置的降级顺序" :delay="60">
      <template #actions>
        <button class="btn small" @click="loadEngines">刷新</button>
      </template>

      <EmptyState v-if="ttsDisabled" title="播报未启用" hint="未配置 [tts].engines" />
      <DataTable
        v-else
        :columns="[
          { key: 'name', label: '引擎', width: '140px' },
          { key: 'enabled', label: '已启用', width: '80px' },
          { key: 'ready', label: '可构造', width: '80px' },
          { key: 'voice', label: '音色' },
          { key: 'model', label: '模型' },
          { key: 'reason', label: '不可用原因' },
        ]"
        :rows="engines"
        empty="没有引擎信息"
      >
        <template #name="{ row }">
          <span class="mono">{{ row.name }}</span>
        </template>
        <template #enabled="{ row }">
          <span class="badge" :class="row.enabled ? 'ok' : ''">{{ row.enabled ? '是' : '否' }}</span>
        </template>
        <template #ready="{ row }">
          <span class="badge" :class="row.ready ? 'ok' : 'err'">{{ row.ready ? '就绪' : '不可用' }}</span>
        </template>
        <template #voice="{ row }">
          <span class="mono dim">{{ row.voice ?? '—' }}</span>
        </template>
        <template #model="{ row }">
          <span class="mono dim">{{ row.model ?? '—' }}</span>
        </template>
        <template #reason="{ row }">
          <span class="faint">{{ row.reason ?? '' }}</span>
        </template>
      </DataTable>
    </PanelCard>

    <PanelCard title="语音试听" hint="POST /api/tts/preview · 不入播报队列，直接回 WAV" :delay="120">
      <EmptyState v-if="ttsDisabled" title="播报未启用" hint="未配置 [tts].engines" />
      <div v-else class="form">
        <textarea v-model="previewText" rows="2" placeholder="试听文本"></textarea>
        <div class="row wrap">
          <select v-model="previewEngine">
            <option value="">默认引擎（降级链）</option>
            <option v-for="engine in engines" :key="engine.name" :value="engine.name">
              {{ engine.name }}
            </option>
          </select>
          <input v-model="previewVoice" type="text" placeholder="音色覆盖（可空）" />
          <button class="btn primary" :disabled="previewing || !previewText.trim()" @click="submitPreview">
            {{ previewing ? '合成中…' : '合成试听' }}
          </button>
        </div>
        <audio v-if="audioURL" :src="audioURL" controls autoplay class="audio"></audio>
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
.form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.form select,
.form input[type='text'] {
  min-width: 150px;
}
.audio {
  width: 100%;
  max-width: 520px;
  height: 36px;
}
</style>
