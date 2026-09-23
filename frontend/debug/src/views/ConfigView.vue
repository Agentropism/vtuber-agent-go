<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getConfig, type ConfigSnapshot } from '../lib/api'
import PanelCard from '../components/PanelCard.vue'
import ConfigNode from '../components/ConfigNode.vue'

const config = ref<ConfigSnapshot | null>(null)
const error = ref('')

async function load(): Promise<void> {
  try {
    config.value = await getConfig()
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

onMounted(load)
</script>

<template>
  <PanelCard
    title="配置快照"
    hint="GET /api/config · 每次请求重读 config.toml；api_key / cookie 已脱敏为 configured 标记"
    :delay="0"
  >
    <template #actions>
      <button class="btn small" @click="load">重新读取</button>
    </template>

    <div v-if="error" class="err mono">{{ error }}</div>
    <div v-if="config" class="tree">
      <ConfigNode v-for="[key, value] in Object.entries(config)" :key="key" :name="key" :value="value" />
    </div>
  </PanelCard>
</template>

<style scoped>
.tree {
  max-height: 70vh;
  overflow: auto;
  padding-right: 6px;
}
</style>
