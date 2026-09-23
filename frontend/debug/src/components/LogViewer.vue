<script setup lang="ts">
import type { LogEntry } from '../lib/api'
import { formatTime } from '../lib/format'

defineProps<{ entries: LogEntry[] }>()
</script>

<template>
  <div class="log-viewer">
    <div v-if="entries.length === 0" class="table-empty">暂无日志</div>
    <div v-for="(entry, index) in entries" :key="index" class="log-row">
      <span class="log-time mono">{{ formatTime(entry.time) }}</span>
      <span class="log-level mono" :class="entry.level.toLowerCase()">{{ entry.level }}</span>
      <span class="log-msg">{{ entry.msg }}</span>
    </div>
  </div>
</template>

<style scoped>
.log-viewer {
  display: flex;
  flex-direction: column;
  max-height: 62vh;
  overflow: auto;
  font-size: 12px;
}
.log-row {
  display: grid;
  grid-template-columns: 118px 54px 1fr;
  gap: 10px;
  padding: 3px 6px;
  border-bottom: 1px solid var(--edge);
}
.log-row:hover {
  background: var(--hover);
}
.log-time {
  color: var(--text-faint);
}
.log-level {
  font-size: 11px;
}
.log-level.debug {
  color: var(--text-faint);
}
.log-level.info {
  color: var(--accent);
}
.log-level.warn {
  color: var(--signal);
}
.log-level.error {
  color: var(--danger);
}
.log-msg {
  word-break: break-all;
}
</style>
