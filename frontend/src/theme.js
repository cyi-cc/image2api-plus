// 固定亮色主题：不再提供光/暗切换，并清掉历史上保存的暗色选择。
import { ref } from 'vue'

localStorage.removeItem('gw_theme')
document.documentElement.classList.remove('dark')

export const isDark = ref(false)
