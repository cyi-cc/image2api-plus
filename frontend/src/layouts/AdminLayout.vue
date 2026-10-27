<script setup>
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { site } from '../site'

const route = useRoute()

const tabs = [
  { label: '概览',    to: '/admin/overview', icon: 'overview' },
  { label: '模型管理', to: '/admin/models',   icon: 'models' },
  { label: '渠道管理', to: '/admin/channels', icon: 'plug' },
  { label: '用户管理', to: '/admin/users',    icon: 'accounts' },
  { label: '并发分组', to: '/admin/concurrency', icon: 'shield' },
  { label: '违禁词管理', icon: 'ban', children: [
    { label: '违禁词列表', to: '/admin/banned-words' },
    { label: '违禁词触发列表', to: '/admin/banned-word-hits' },
  ] },
  { label: '订单管理', to: '/admin/orders',   icon: 'receipt' },
  { label: '兑换码管理', to: '/admin/cdks',   icon: 'spark' },
  { label: '邀请日志', to: '/admin/invites',  icon: 'accounts' },
  { label: '作品管理', to: '/admin/images',   icon: 'files' },
  { label: '首页内容', to: '/admin/showcase', icon: 'spark' },
  { label: '日志管理', to: '/admin/logs',     icon: 'log' },
  { label: '系统配置', to: '/admin/config',   icon: 'config' },
]

// 二级菜单展开状态：当前路由命中子项时默认展开，也可手动切换。
const openGroups = ref(new Set())
function groupActive(t) { return (t.children || []).some((c) => route.path.startsWith(c.to)) }
function toggleGroup(label) {
  const s = new Set(openGroups.value)
  s.has(label) ? s.delete(label) : s.add(label)
  openGroups.value = s
}
watch(() => route.path, () => {
  for (const t of tabs) {
    if (t.children && groupActive(t) && !openGroups.value.has(t.label)) {
      const s = new Set(openGroups.value); s.add(t.label); openGroups.value = s
    }
  }
}, { immediate: true })
</script>

<template>
  <div class="theme-x h-screen flex bg-[var(--app-bg)] text-[color:var(--fg-2)] selection:bg-orange-400/30 overflow-hidden">
    <!-- ===== Sidebar ===== -->
    <aside class="w-60 shrink-0 border-r border-r-[color:var(--ink-soft)] bg-white flex flex-col">
      <router-link to="/" class="h-16 flex items-center gap-2.5 px-5 border-b border-b-[color:var(--ink-soft)] group">
        <img v-if="site.logo" :src="site.logo" :alt="site.title" class="w-8 h-8 rounded-[10px] object-contain ring-1 ring-[color:var(--ink)]" />
        <Logo v-else :size="32" />
        <div class="leading-tight min-w-0">
          <div class="text-sm font-extrabold truncate tracking-tight text-[color:var(--fg)]">{{ site.title }}</div>
          <div class="admin-tag text-[11px] font-bold uppercase tracking-widest truncate">Admin</div>
        </div>
      </router-link>

      <nav class="flex-1 px-3 py-4 space-y-1 overflow-y-auto">
        <template v-for="t in tabs" :key="t.label">
          <router-link v-if="!t.children" :to="t.to"
            class="admin-link group"
            active-class="active">
            <span class="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 rounded-full opacity-0 transition-opacity"
                  style="background: #18181b"></span>
            <Icon :name="t.icon" class="w-4 h-4 shrink-0 opacity-70 group-hover:opacity-100 transition-opacity" />
            <span class="text-sm">{{ t.label }}</span>
          </router-link>
          <div v-else>
            <button type="button" @click="toggleGroup(t.label)"
                    class="admin-link group w-full text-left" :class="groupActive(t) && 'active'">
              <span class="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 rounded-full opacity-0 transition-opacity"
                    style="background: #18181b"></span>
              <Icon :name="t.icon" class="w-4 h-4 shrink-0 opacity-70 group-hover:opacity-100 transition-opacity" />
              <span class="text-sm">{{ t.label }}</span>
              <svg class="w-3 h-3 ml-auto transition-transform" :class="openGroups.has(t.label) && 'rotate-90'"
                   viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18l6-6-6-6"/></svg>
            </button>
            <div v-if="openGroups.has(t.label)" class="mt-1 space-y-0.5">
              <router-link v-for="c in t.children" :key="c.to" :to="c.to"
                class="admin-sublink" active-class="active">
                <span class="text-sm">{{ c.label }}</span>
              </router-link>
            </div>
          </div>
        </template>
      </nav>

      <div class="p-3 border-t border-t-[color:var(--ink-soft)] space-y-1">
        <router-link to="/user"
                     class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-xs text-[color:var(--fg-2)] hover:bg-[var(--hover)] hover:text-[color:var(--fg)] transition-colors">
          <Icon name="spark" class="w-3.5 h-3.5" /> 用户端
          <Icon name="open" class="w-3 h-3 ml-auto" />
        </router-link>
      </div>
    </aside>

    <!-- ===== Main ===== -->
    <div class="flex-1 min-w-0 flex flex-col relative">

      <main :class="['theme-text flex-1 overflow-y-auto overscroll-y-none relative z-10 [scrollbar-gutter:stable]']">
        <div class="px-8 py-8 max-w-[1500px] mx-auto">
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.admin-tag {
  background: linear-gradient(120deg, #18181b, #52525b);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.admin-link {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.55rem 0.875rem;
  border-radius: 0.625rem;
  border: 1px solid transparent;
  color: var(--fg-2);
  font-weight: 600;
  transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease;
}
.admin-link:hover { background: rgb(24 24 27 / 0.05); color: var(--fg); }
.admin-link.active {
  color: #18181b;
  background: rgb(24 24 27 / 0.07);
  font-weight: 800;
}
.admin-link.active > :deep(svg) { opacity: 1; }
.admin-link.active > span:first-child { opacity: 1 !important; }

.admin-sublink {
  display: flex;
  align-items: center;
  padding: 0.45rem 0.875rem 0.45rem 2.6rem;
  border-radius: 0.625rem;
  border: 1px solid transparent;
  color: var(--fg-2);
  font-weight: 600;
  transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
}
.admin-sublink:hover { background: rgb(24 24 27 / 0.05); color: var(--fg); }
.admin-sublink.active { color: #18181b; background: rgb(24 24 27 / 0.07); font-weight: 800; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.12s ease; }
.fade-enter-from { opacity: 0; }
.fade-leave-to { opacity: 0; }
</style>
