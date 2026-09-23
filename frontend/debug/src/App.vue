<script setup lang="ts">
import SideNav from './components/SideNav.vue'
import StatusStrip from './components/StatusStrip.vue'
import ToastHost from './components/ToastHost.vue'
</script>

<template>
  <div class="shell">
    <SideNav />
    <div class="main">
      <StatusStrip />
      <main class="content">
        <RouterView />
      </main>
    </div>
    <ToastHost />
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: var(--sidenav-w) 1fr;
  height: 100%;
}
.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.content {
  flex: 1;
  overflow: auto;
  padding: var(--gap) 18px 28px;
}
</style>
