<script setup lang="ts">
import { panels } from '../router'
</script>

<template>
  <aside class="sidenav">
    <div class="brand">
      <div class="brand-title">导播中控台</div>
      <div class="brand-sub mono">vtuber-agent-go</div>
    </div>

    <nav class="nav">
      <RouterLink
        v-for="panel in panels"
        :key="panel.path"
        :to="panel.path"
        class="nav-item"
        active-class="active"
      >
        <span class="nav-index mono">{{ panel.index }}</span>
        <span class="nav-title">{{ panel.title }}</span>
        <span class="nav-hint">{{ panel.hint }}</span>
      </RouterLink>
    </nav>

    <div class="foot">
      <a href="/" target="_blank" rel="noreferrer">舞台页 ↗</a>
      <span class="faint">只允许本机访问</span>
    </div>
  </aside>
</template>

<style scoped>
.sidenav {
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--edge);
  background: var(--bg-raise);
  min-height: 0;
}
.brand {
  padding: 16px 16px 14px;
  border-bottom: 1px solid var(--edge);
}
.brand-title {
  font-family: var(--font-display);
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.12em;
  color: var(--accent);
}
.brand-sub {
  font-size: 11px;
  color: var(--text-faint);
  letter-spacing: 0.06em;
}
.nav {
  flex: 1;
  overflow: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.nav-item {
  display: grid;
  grid-template-columns: 26px 1fr;
  grid-template-rows: auto auto;
  column-gap: 8px;
  padding: 7px 9px;
  border-radius: 4px;
  border: 1px solid transparent;
  color: var(--text);
  text-decoration: none;
  transition: background 0.15s ease, border-color 0.15s ease;
}
.nav-item:hover {
  background: var(--hover);
  text-decoration: none;
}
.nav-item.active {
  background: var(--accent-soft);
  border-color: var(--accent-line);
}
.nav-index {
  grid-row: span 2;
  align-self: center;
  font-size: 12px;
  color: var(--text-faint);
}
.nav-item.active .nav-index {
  color: var(--accent);
}
.nav-title {
  font-size: 13px;
  line-height: 1.3;
}
.nav-hint {
  font-size: 11px;
  color: var(--text-faint);
  line-height: 1.3;
}
.foot {
  padding: 12px 16px;
  border-top: 1px solid var(--edge);
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 12px;
}
</style>
