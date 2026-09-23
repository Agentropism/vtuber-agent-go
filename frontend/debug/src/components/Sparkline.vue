<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    values: number[]
    width?: number
    height?: number
    color?: string
  }>(),
  { width: 140, height: 30, color: 'var(--accent)' },
)

// 把数值序列映射成折线；空序列不画线。
const points = computed(() => {
  const values = props.values
  if (values.length === 0) return ''

  const max = Math.max(...values, 1)
  const step = values.length > 1 ? props.width / (values.length - 1) : props.width

  return values
    .map((value, index) => {
      const x = index * step
      const y = props.height - 2 - (value / max) * (props.height - 6)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})
</script>

<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" class="sparkline">
    <line
      v-if="values.length === 0"
      :x1="2"
      :y1="height - 2"
      :x2="width - 2"
      :y2="height - 2"
      stroke="var(--edge-strong)"
      stroke-dasharray="3 3"
    />
    <polyline
      v-else
      :points="points"
      fill="none"
      :stroke="color"
      stroke-width="1.5"
      stroke-linejoin="round"
      stroke-linecap="round"
    />
  </svg>
</template>

<style scoped>
.sparkline {
  display: block;
}
</style>
