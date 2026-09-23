<script setup lang="ts">
defineProps<{ title: string; hint?: string; delay?: number }>()
</script>

<template>
  <section
    class="panel"
    :style="delay !== undefined ? { animationDelay: `${delay}ms` } : undefined"
  >
    <header class="panel-head">
      <h2>{{ title }}</h2>
      <span v-if="hint" class="panel-hint">{{ hint }}</span>
      <span class="spacer"></span>
      <slot name="actions"></slot>
    </header>
    <div class="panel-body">
      <slot></slot>
    </div>
  </section>
</template>
