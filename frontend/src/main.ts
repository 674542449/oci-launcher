import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './assets/main.css'

// Naive UI components are imported per-file so unused ones are tree-shaken.
const app = createApp(App)

app.use(createPinia())
app.use(router)

app.mount('#app')
