<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { auth, isAuthed, isAdmin, openLogin, openRegister, logout } from '../auth'
import { isDark, toggleTheme } from '../theme'
import { site } from '../site'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { pointsLabel } from '../credits'
import { draft } from '../playground'
import { announcement, openAnnouncement } from '../announcement'

const route = useRoute()

// Top-bar links. 画图/图片 only show once signed in; everything else a
// signed-in user needs lives in the avatar menu so the bar stays short.
const nav = computed(() => {
  const items = [{ to: '/', label: '首页' }]
  if (isAuthed()) {
    items.push({ to: '/user', label: '画图' })
    items.push({ to: '/canvas', label: '画布' })
    items.push({ to: '/logs', label: '图片' })
  }
  items.push({ to: '/docs', label: '文档' })
  items.push({ to: '/about', label: '关于' })
  return items
})

// Avatar menu — only the surfaces the top nav doesn't already expose
// (画图/图片 live in the bar, so they're intentionally left out here).
const userMenu = [
  { to: '/mylogs', label: '生成日志' },
  { to: '/invite', label: '邀请返利' },
  { to: '/orders', label: '订单' },
  { to: '/settings', label: '设置' },
]

const menuOpen = ref(false)     // avatar dropdown
const mobileOpen = ref(false)   // hamburger panel
watch(() => route.fullPath, () => { menuOpen.value = false; mobileOpen.value = false })

// credits — the logged-in user's real server-side balance (auth.user.credits)
const credits = computed(() => Number(auth.user?.credits || 0))
const creditsLabel = computed(() => pointsLabel(credits.value))
const displayName = computed(() => {
  const n = (auth.user?.name || '').trim()
  if (n) return n
  const e = (auth.user?.email || '').trim()
  return e ? e.split('@')[0] : '我的'
})

// On the 画图 workbench the label tracks the active mode — it flips with the
// 生图/生视频 tab, and on state restore reflects a pending job's kind.
const currentLabel = computed(() => {
  if (route.path === '/user') return draft.mode === 'video' ? '生视频' : '生图'
  return route.meta?.label || ''
})

// The home page paints its own full-bleed hero; other pages keep the padded
// shell plus a route-label header.
const isHome = computed(() => route.path === '/')

async function onLogout() {
  menuOpen.value = false
  await logout()
}
</script>

<template>
  <div class="theme-x min-h-screen bg-[var(--app-bg)] text-[color:var(--fg-2)] selection:bg-orange-400/30">
    <!-- ===== Top bar ===== -->
    <header class="fixed top-0 inset-x-0 z-40 backdrop-blur-xl top-bar">
      <div class="mx-auto max-w-[1400px] px-4 md:px-8 h-[58px] flex items-center gap-3">
        <!-- brand -->
        <router-link to="/" class="flex items-center gap-2.5 shrink-0 transition-transform hover:scale-[1.03]">
          <img v-if="site.logo" :src="site.logo" :alt="site.title" class="w-8 h-8 object-contain" />
          <Logo v-else :size="30" />
          <span class="text-[17px] font-bold tracking-tight text-[color:var(--fg)]">{{ site.title }}</span>
          <span class="brand-spark" aria-hidden="true">✦</span>
        </router-link>

        <!-- links (desktop) -->
        <nav class="hidden md:flex items-center gap-0.5 ml-4">
          <router-link v-for="n in nav" :key="n.to" :to="n.to"
                       :exact-active-class="n.to === '/' ? 'on' : ''"
                       :active-class="n.to === '/' ? '' : 'on'"
                       class="top-link">{{ n.label }}</router-link>
        </nav>

        <div class="flex-1"></div>

        <!-- balance (signed in) -->
        <router-link v-if="isAuthed()" to="/settings"
                     class="hidden sm:block text-xs text-[color:var(--fg-2)] hover:text-[color:var(--fg)] tabular-nums transition-colors">
          余额 <span class="text-[color:var(--fg)] font-semibold">{{ creditsLabel }}</span>
        </router-link>

        <!-- theme -->
        <button type="button" @click="toggleTheme" class="round-btn"
                :title="isDark ? '切换到亮色' : '切换到暗色'">
          <svg v-if="isDark" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
          <svg v-else class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
        </button>

        <!-- 公告 -->
        <button v-if="isAuthed() && announcement.content.trim()" type="button"
                @click="openAnnouncement" class="round-btn" title="公告">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 11l18-7-4 14-6-3-3 4-1-6z"/></svg>
        </button>

        <!-- admin shortcut -->
        <router-link v-if="isAdmin()" to="/admin/overview" class="round-btn" title="进入管理后台">
          <Icon name="shield" class="w-4 h-4" />
        </router-link>

        <!-- guest actions -->
        <template v-if="!isAuthed()">
          <button type="button" class="hidden sm:inline-flex top-link" @click="openLogin('/user')">登录</button>
          <button type="button" class="solid-btn" @click="openRegister('', '/user')">
            免费开始 <span aria-hidden="true">→</span>
          </button>
        </template>

        <!-- signed-in: avatar menu -->
        <div v-else class="relative">
          <button type="button" class="name-btn" @click.stop="menuOpen = !menuOpen" title="我的">
            <span class="name-txt">{{ displayName }}</span>
            <svg class="w-3.5 h-3.5 shrink-0 transition-transform" :class="menuOpen && 'rotate-180'"
                 viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
          </button>
          <div v-if="menuOpen" class="menu" @click.stop>
            <router-link v-for="m in userMenu" :key="m.to" :to="m.to" class="menu-item">{{ m.label }}</router-link>
            <div class="menu-sep"></div>
            <button type="button" class="menu-item w-full text-left" @click="onLogout">退出登录</button>
          </div>
        </div>

        <!-- hamburger -->
        <button type="button" class="round-btn hamburger" @click.stop="mobileOpen = !mobileOpen" aria-label="菜单">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 7h16M4 12h16M4 17h16"/></svg>
        </button>
      </div>

      <!-- mobile panel -->
      <div v-if="mobileOpen" class="md:hidden border-t border-[color:var(--ink-soft)] px-3 py-3 bg-[var(--menu-bg)]">
        <router-link v-for="n in nav" :key="n.to" :to="n.to"
                     :exact-active-class="n.to === '/' ? 'm-on' : ''"
                     :active-class="n.to === '/' ? '' : 'm-on'"
                     class="m-link">{{ n.label }}</router-link>
        <template v-if="isAuthed()">
          <div class="menu-sep"></div>
          <router-link v-for="m in userMenu" :key="m.to" :to="m.to" active-class="m-on" class="m-link">{{ m.label }}</router-link>
        </template>
        <template v-else>
          <div class="menu-sep"></div>
          <button type="button" class="m-link w-full text-left" @click="openLogin('/user')">登录</button>
        </template>
      </div>
    </header>

    <!-- click-away for the dropdowns -->
    <div v-if="menuOpen || mobileOpen" class="fixed inset-0 z-30"
         @click="menuOpen = false; mobileOpen = false"></div>

    <!-- ===== Main column ===== -->
    <div class="relative">
      <!-- aurora backdrop: drifting gradient orbs + faint grain -->
      <div aria-hidden="true" class="pointer-events-none absolute inset-0 overflow-hidden">
        <div class="memphis-dots absolute inset-0 opacity-50"></div>
        <div class="absolute -top-40 left-1/4 w-[42rem] h-[42rem] rounded-full opacity-[0.5]"
             style="background: radial-gradient(circle, rgb(139 92 246 / 0.35), transparent 60%); filter: blur(100px)"></div>
        <div class="absolute top-1/3 -right-48 w-[38rem] h-[38rem] rounded-full opacity-[0.45]"
             style="background: radial-gradient(circle, rgb(34 211 238 / 0.3), transparent 60%); filter: blur(100px)"></div>
        <div class="absolute bottom-0 -left-24 w-[34rem] h-[34rem] rounded-full opacity-[0.4]"
             style="background: radial-gradient(circle, rgb(232 121 249 / 0.28), transparent 60%); filter: blur(100px)"></div>
        <div class="anime-stars"></div>
      </div>

      <!-- Route label header — the home page brings its own hero instead. -->
      <header v-if="!isHome" class="relative z-10 mx-auto max-w-[1400px] px-4 md:px-8 pt-[86px] pb-4">
        <span class="text-[22px] font-bold tracking-tight text-[color:var(--fg)]">{{ currentLabel }}</span>
      </header>

      <main :class="['relative z-10 mx-auto max-w-[1400px] pb-24',
                     isHome ? 'pt-[58px] px-0' : 'px-4 md:px-8 pt-2',
                     { 'public-dark': isDark }]">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.top-bar {
  background: color-mix(in srgb, var(--app-bg) 62%, transparent);
  border-bottom: 1px solid var(--ink-soft);
  transition: border-color 0.2s ease;
}

.top-link {
  padding: 0.45rem 0.8rem;
  border-radius: 0.6rem;
  font-size: 13.5px;
  font-weight: 600;
  color: var(--fg-2);
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease;
}
.top-link:hover { color: var(--fg); background: var(--hover); border-color: rgb(124 92 252 / 0.3); }
.top-link.on {
  color: #fff;
  font-weight: 700;
  background: linear-gradient(120deg, #7c5cfc, #5f6cf8);
  border-color: transparent;
  box-shadow: 0 4px 14px var(--accent-glow);
}

.brand-spark {
  font-size: 12px;
  line-height: 1;
  color: var(--m-orange);
  animation: brandSpark 3.5s ease-in-out infinite;
}
@keyframes brandSpark {
  0%, 100% { opacity: 0.55; transform: scale(0.9) rotate(0deg); }
  50% { opacity: 1; transform: scale(1.15) rotate(20deg); }
}
@media (prefers-reduced-motion: reduce) { .brand-spark { animation: none; } }

.m-link {
  display: block;
  padding: 0.6rem 0.75rem;
  border-radius: 0.6rem;
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-2);
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease;
}
.m-link:hover { color: var(--fg); background: var(--hover); border-color: rgb(124 92 252 / 0.3); }
.m-link.m-on {
  color: #fff;
  font-weight: 700;
  background: linear-gradient(120deg, #7c5cfc, #5f6cf8);
  border-color: transparent;
  box-shadow: 0 4px 14px var(--accent-glow);
}

.round-btn {
  width: 2.125rem;
  height: 2.125rem;
  border-radius: 0.6rem;
  display: grid;
  place-items: center;
  color: var(--fg-3);
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  border: 1px solid var(--ink-soft);
  transition: color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease, border-color 0.15s ease;
}
.round-btn:hover { color: var(--fg); background: var(--hover); border-color: rgb(124 92 252 / 0.4); box-shadow: 0 4px 14px rgb(124 92 252 / 0.16); transform: translateY(-1px); }
.round-btn:active { transform: scale(0.95) translateY(1px); box-shadow: none; }

/* mobile-only: .round-btn's display:grid outranks Tailwind's md:hidden, so
   the breakpoint lives here instead */
@media (min-width: 768px) { .hamburger { display: none; } }

.solid-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.5rem 1.1rem;
  border-radius: 0.65rem;
  font-size: 13.5px;
  font-weight: 800;
  color: #fff;
  background: linear-gradient(120deg, #7c5cfc, #5f6cf8 55%, #22d3ee 140%);
  border: 1px solid rgb(255 255 255 / 0.16);
  cursor: pointer;
  box-shadow: 0 6px 18px var(--accent-glow);
  transition: transform 0.15s ease, filter 0.15s ease, box-shadow 0.15s ease;
}
.solid-btn:hover { filter: brightness(1.07); transform: translateY(-1px); box-shadow: 0 10px 26px var(--accent-glow); }
.solid-btn:active { transform: scale(0.97) translateY(1px); box-shadow: 0 3px 10px var(--accent-glow); }

.name-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  max-width: 10rem;
  height: 2.125rem;
  padding: 0 0.7rem 0 0.85rem;
  border-radius: 0.6rem;
  border: 1px solid var(--ink-soft);
  cursor: pointer;
  font-size: 13px;
  font-weight: 700;
  color: var(--fg);
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  transition: background 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease, border-color 0.15s ease;
}
.name-btn:hover {
  background: var(--hover);
  border-color: rgb(124 92 252 / 0.4);
  box-shadow: 0 4px 14px rgb(124 92 252 / 0.16);
  transform: translateY(-1px);
}
.name-txt {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.menu {
  position: absolute;
  right: 0;
  top: 2.75rem;
  width: 11.5rem;
  padding: 0.4rem;
  border-radius: 0.85rem;
  background: var(--menu-bg);
  -webkit-backdrop-filter: blur(18px);
  backdrop-filter: blur(18px);
  border: 1px solid var(--glass-border);
  box-shadow: var(--glass-shadow-lg);
  z-index: 50;
}
.menu-item {
  display: block;
  padding: 0.5rem 0.65rem;
  border-radius: 0.5rem;
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-2);
  background: transparent;
  border: none;
  cursor: pointer;
}
.menu-item:hover { background: var(--hover); color: var(--fg); }
.menu-sep { height: 1px; background: var(--hairline); margin: 0.35rem 0.25rem; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.fade-enter-from { opacity: 0; transform: translateY(8px); }
.fade-leave-to { opacity: 0; }
</style>
