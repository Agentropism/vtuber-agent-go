<script setup lang="ts">
import { useToasts } from '../lib/toast'

const toasts = useToasts()
</script>

<template>
  <div class="toast-host">
    <TransitionGroup name="toast">
      <div v-for="item in toasts.items" :key="item.id" class="toast" :class="item.kind">
        {{ item.text }}
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-host {
  position: fixed;
  right: 18px;
  bottom: 18px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  z-index: 40;
  pointer-events: none;
}
.toast {
  padding: 8px 14px;
  border-radius: 4px;
  background: var(--panel);
  border: 1px solid var(--edge-strong);
  border-left-width: 3px;
  font-size: 12.5px;
  max-width: 420px;
  box-shadow: var(--shadow);
}
.toast.ok {
  border-left-color: var(--ok);
}
.toast.err {
  border-left-color: var(--danger);
}
.toast.info {
  border-left-color: var(--accent);
}
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateY(8px);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(12px);
}
</style>
