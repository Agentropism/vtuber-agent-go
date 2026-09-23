<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ name: string; value: unknown }>()

function isBranch(value: unknown): boolean {
  return value !== null && typeof value === 'object'
}

function entries(value: unknown): [string, unknown][] {
  if (!isBranch(value)) return []
  return Object.entries(value as Record<string, unknown>)
}

function leaf(value: unknown): string {
  if (value === null) return 'null'
  if (typeof value === 'string') return value === '' ? '""' : value
  return String(value)
}

const childEntries = computed(() => entries(props.value))
</script>

<template>
  <div class="node">
    <details v-if="isBranch(value)" open>
      <summary class="mono">{{ name }}</summary>
      <div class="children">
        <ConfigNode
          v-for="[key, child] in childEntries"
          :key="key"
          :name="String(key)"
          :value="child"
        />
      </div>
    </details>
    <div v-else class="leaf">
      <span class="leaf-name mono">{{ name }}</span>
      <span class="leaf-value mono">{{ leaf(value) }}</span>
    </div>
  </div>
</template>

<style scoped>
.node {
  font-size: 12.5px;
}
summary {
  cursor: pointer;
  color: var(--accent);
  padding: 2px 0;
}
.children {
  margin-left: 14px;
  border-left: 1px solid var(--edge);
  padding-left: 10px;
}
.leaf {
  display: grid;
  grid-template-columns: minmax(140px, max-content) 1fr;
  gap: 12px;
  padding: 1px 0;
}
.leaf-name {
  color: var(--text-faint);
}
.leaf-value {
  word-break: break-all;
}
</style>
