<script setup lang="ts">
defineProps<{
  state?: 'ok' | 'warn' | 'err' | 'idle' | 'on'
  label?: string
}>()
</script>

<template>
  <span class="led-wrap">
    <span class="led" :class="state ?? 'idle'"></span>
    <span v-if="label" class="led-label">{{ label }}</span>
  </span>
</template>

<style scoped>
.led-wrap {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.led {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--led-idle);
  flex: none;
}
.led.ok {
  background: var(--ok);
  animation: led-breathe 2.6s ease-in-out infinite;
}
.led.warn {
  background: var(--signal);
}
.led.err {
  background: var(--danger);
}
.led.on {
  background: var(--danger);
  animation: led-breathe 1.1s ease-in-out infinite;
}
.led-label {
  color: var(--text-dim);
  font-size: 12px;
}
@keyframes led-breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}
</style>
