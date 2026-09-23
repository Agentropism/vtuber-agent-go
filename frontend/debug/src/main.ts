import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { initTheme } from './lib/theme'
import './styles/tokens.css'
import './styles/base.css'
import './styles/panel.css'

// 先定主题再挂载：避免首屏闪一下默认配色
initTheme()

createApp(App).use(router).mount('#app')
