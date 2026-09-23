<script setup lang="ts">
defineProps<{ title: string; hint?: string }>()
</script>

<template>
  <div class="empty-state">
    <div class="empty-title">{{ title }}</div>
    <div v-if="hint" class="empty-hint">{{ hint }}</div>
    <div class="empty-actions">
      <slot></slot>
    </div>
  </div>
</template>

<style scoped>
.empty-actions:not(:empty) {
  margin-top: 10px;
}
</style>
