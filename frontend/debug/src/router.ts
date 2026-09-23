import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

// 面板清单：顺序即导航顺序，index 是导播台的通道号。
export interface PanelMeta {
  path: string
  index: string
  title: string
  hint: string
}

export const panels: PanelMeta[] = [
  { path: '/overview', index: '01', title: '概览', hint: '运行状态总览' },
  { path: '/sessions', index: '02', title: '会话', hint: '渠道会话与历史' },
  { path: '/broadcast', index: '03', title: '播报', hint: '语音播报与试听' },
  { path: '/models', index: '04', title: '模型', hint: 'Live2D 与表情' },
  { path: '/memory', index: '05', title: '记忆', hint: '长期记忆读写' },
  { path: '/tools', index: '06', title: '工具', hint: '工具注册与直调' },
  { path: '/config', index: '07', title: '配置', hint: '配置快照（脱敏）' },
  { path: '/logs', index: '08', title: '日志', hint: '进程日志流' },
  { path: '/stream', index: '09', title: '推流', hint: '开播与推流控制' },
  { path: '/chats', index: '10', title: '对话记录', hint: '归档检索与导出' },
]

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/overview' },
  { path: '/overview', component: () => import('./views/OverviewView.vue'), meta: { title: '概览' } },
  { path: '/sessions', component: () => import('./views/SessionsView.vue'), meta: { title: '会话' } },
  { path: '/broadcast', component: () => import('./views/BroadcastView.vue'), meta: { title: '播报' } },
  { path: '/models', component: () => import('./views/ModelsView.vue'), meta: { title: '模型' } },
  { path: '/memory', component: () => import('./views/MemoryView.vue'), meta: { title: '记忆' } },
  { path: '/tools', component: () => import('./views/ToolsView.vue'), meta: { title: '工具' } },
  { path: '/config', component: () => import('./views/ConfigView.vue'), meta: { title: '配置' } },
  { path: '/logs', component: () => import('./views/LogsView.vue'), meta: { title: '日志' } },
  { path: '/stream', component: () => import('./views/StreamView.vue'), meta: { title: '推流' } },
  { path: '/chats', component: () => import('./views/ChatArchiveView.vue'), meta: { title: '对话记录' } },
  { path: '/:pathMatch(.*)*', redirect: '/overview' },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
