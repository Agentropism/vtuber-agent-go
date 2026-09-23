<script setup lang="ts" generic="T extends Record<string, unknown>">
defineProps<{
  columns: { key: string; label: string; width?: string }[]
  rows: T[]
  empty?: string
  clickable?: boolean
  rowKey?: string
  activeKey?: string | number | null
}>()

const emit = defineEmits<{ row: [row: T] }>()
</script>

<template>
  <table class="table" :class="{ clickable }">
    <thead>
      <tr>
        <th
          v-for="col in columns"
          :key="col.key"
          :style="col.width ? { width: col.width } : undefined"
        >
          {{ col.label }}
        </th>
      </tr>
    </thead>
    <tbody>
      <tr v-if="rows.length === 0">
        <td :colspan="columns.length" class="table-empty">{{ empty ?? '暂无数据' }}</td>
      </tr>
      <tr
        v-for="(row, index) in rows"
        :key="rowKey ? String(row[rowKey]) : index"
        :class="{
          active:
            rowKey !== undefined &&
            activeKey !== undefined &&
            activeKey !== null &&
            String(row[rowKey]) === String(activeKey),
        }"
        @click="clickable ? emit('row', row) : undefined"
      >
        <td v-for="col in columns" :key="col.key">
          <slot :name="col.key" :row="row">{{ row[col.key] ?? '—' }}</slot>
        </td>
      </tr>
    </tbody>
  </table>
</template>
