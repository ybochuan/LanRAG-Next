import { createApp } from 'vue'
import { Button, Card, Input, Alert, Loading } from 'tdesign-vue-next'
import { Icon } from 'tdesign-icons-vue-next'
import '../../frontend/node_modules/tdesign-vue-next/es/style/index.css'
import '@/assets/theme/theme.css'
import i18n from '@/i18n'
import App from './App.vue'

const app = createApp(App)
for (const [name, component] of Object.entries({ Button, Card, Input, Alert, Loading, Icon })) {
  app.component(`t-${name.toLowerCase()}`, component)
}
app.use(i18n).mount('#app')
