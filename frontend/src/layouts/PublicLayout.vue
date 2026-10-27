<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { auth, isAuthed, isAdmin, openLogin, openRegister, logout } from '../auth'
import { site } from '../site'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { pointsLabel } from '../credits'
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
  { to: '/mylogs', label: '生成日志', icon: 'log' },
  { to: '/invite', label: '邀请返利', icon: 'team' },
  { to: '/orders', label: '订单', icon: 'receipt' },
  { to: '/settings', label: '设置', icon: 'config' },
]

const menuOpen = ref(false)     // avatar dropdown
const mobileOpen = ref(false)   // hamburger panel
watch(() => route.fullPath, () => { menuOpen.value = false; mobileOpen.value = false })

// 悬浮胶囊顶栏：静止时浮在页面上方（999px 圆角），滚动后展开为通栏。
const scrolled = ref(false)
function onScroll() { scrolled.value = window.scrollY > 4 }
onMounted(() => { onScroll(); window.addEventListener('scroll', onScroll, { passive: true }) })
onUnmounted(() => window.removeEventListener('scroll', onScroll))

// credits — the logged-in user's real server-side balance (auth.user.credits)
const credits = computed(() => Number(auth.user?.credits || 0))
const creditsLabel = computed(() => pointsLabel(credits.value))
const avatarChar = computed(() => Array.from(displayName.value)[0]?.toUpperCase() || '?')
const displayName = computed(() => {
  const n = (auth.user?.name || '').trim()
  if (n) return n
  const e = (auth.user?.email || '').trim()
  return e ? e.split('@')[0] : '我的'
})

// The home page paints its own full-bleed hero; other pages keep the padded
// shell.
const isHome = computed(() => route.path === '/')

// CanvasView's root is a <Teleport to="body"> — keep it outside the
// out-in transition so leaving the canvas cannot stall the next page.

async function onLogout() {
  menuOpen.value = false
  await logout()
}
</script>

<template>
  <div class="theme-x min-h-screen bg-[var(--app-bg)] text-[color:var(--fg-2)] selection:bg-orange-400/30"
       :class="{ 'home-bg': isHome }">
    <!-- ===== Top bar：悬浮胶囊，滚动展开为通栏 ===== -->
    <header class="top-nav fixed top-0 inset-x-0 z-40" :class="{ scrolled }">
      <div class="nav-inner mx-auto flex items-center gap-3">
        <!-- brand -->
        <router-link to="/" class="flex items-center gap-2.5 shrink-0 transition-transform hover:scale-[1.03]">
          <img v-if="site.logo" :src="site.logo" :alt="site.title" class="w-8 h-8 object-contain" />
          <Logo v-else :size="30" />
          <span class="brand-word text-[19px]">{{ site.title }}</span>
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

        <!-- 公告 -->
        <button v-if="isAuthed() && announcement.content.trim()" type="button"
                @click="openAnnouncement" class="round-btn" title="公告">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 11l18-7-4 14-6-3-3 4-1-6z"/></svg>
        </button>

        <!-- guest actions -->
        <template v-if="!isAuthed()">
          <button type="button" class="login-pill" @click="openLogin('/user')">登录 / 注册</button>
        </template>

        <!-- signed-in: avatar menu（余额 / 后台入口都收进来） -->
        <div v-else class="relative">
          <button type="button" class="name-btn" :class="{ open: menuOpen }" @click.stop="menuOpen = !menuOpen" title="我的">
            <span class="avatar">{{ avatarChar }}</span>
            <span class="name-txt">{{ displayName }}</span>
            <svg class="w-3.5 h-3.5 shrink-0 text-[color:var(--fg-3)] transition-transform" :class="menuOpen && 'rotate-180'"
                 viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
          </button>
          <div v-if="menuOpen" class="menu" @click.stop>
            <div class="menu-head">
              <span class="avatar avatar--lg">{{ avatarChar }}</span>
              <div class="min-w-0">
                <div class="menu-name">{{ displayName }}</div>
                <div v-if="auth.user?.email" class="menu-mail">{{ auth.user.email }}</div>
              </div>
            </div>
            <router-link to="/settings" class="menu-balance">
              <span class="mb-label">余额</span>
              <span class="mb-val">{{ creditsLabel }}</span>
              <span class="mb-go">充值 →</span>
            </router-link>
            <router-link v-for="m in userMenu" :key="m.to" :to="m.to" class="menu-item">
              <Icon :name="m.icon" :size="15" /> {{ m.label }}
            </router-link>
            <router-link v-if="isAdmin()" to="/admin/overview" class="menu-item">
              <Icon name="shield" :size="15" /> 管理后台
            </router-link>
            <div class="menu-sep"></div>
            <button type="button" class="menu-item menu-item--out w-full text-left" @click="onLogout">退出登录</button>
          </div>
        </div>

        <!-- hamburger -->
        <button type="button" class="round-btn hamburger" @click.stop="mobileOpen = !mobileOpen" aria-label="菜单">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 7h16M4 12h16M4 17h16"/></svg>
        </button>
      </div>

      <!-- mobile panel -->
      <div v-if="mobileOpen" class="mobile-panel md:hidden px-3 py-3">
        <router-link v-for="n in nav" :key="n.to" :to="n.to"
                     :exact-active-class="n.to === '/' ? 'm-on' : ''"
                     :active-class="n.to === '/' ? '' : 'm-on'"
                     class="m-link">{{ n.label }}</router-link>
        <template v-if="isAuthed()">
          <div class="menu-sep"></div>
          <router-link v-for="m in userMenu" :key="m.to" :to="m.to" active-class="m-on" class="m-link">{{ m.label }}</router-link>
          <router-link v-if="isAdmin()" to="/admin/overview" class="m-link">管理后台</router-link>
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

      <main :class="['relative z-10 mx-auto max-w-[1400px] pb-24',
                     isHome ? 'pt-0 px-0' : 'px-4 md:px-8 pt-[96px]']">
        <router-view v-slot="{ Component, route: viewRoute }">
          <component :is="Component" v-if="viewRoute.path === '/canvas'" />
          <transition v-else name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
/* 悬浮胶囊导航：不压满宽黑线，毛玻璃胶囊浮在页面上方；滚动后展开为通栏 */
.top-nav {
  padding-top: 10px;
  background: transparent;
  pointer-events: none;
  transition: padding-top 0.22s ease;
}
.nav-inner {
  pointer-events: auto;
  height: 64px;
  width: min(1200px, calc(100% - 40px));
  padding: 0 14px 0 20px;
  border-radius: 999px;
  background: var(--menu-bg);
  -webkit-backdrop-filter: blur(20px) saturate(1.7);
  backdrop-filter: blur(20px) saturate(1.7);
  border: 1px solid var(--ink-soft);
  box-shadow: 0 8px 30px -14px rgb(15 23 42 / 0.18);
  transition: width 0.25s ease, border-radius 0.25s ease, border-color 0.25s ease,
    box-shadow 0.25s ease, background 0.25s ease;
}
.top-nav.scrolled { padding-top: 0; }
.top-nav.scrolled .nav-inner {
  width: 100%;
  border-radius: 0;
  border-color: transparent;
  border-bottom: 1px solid var(--ink-soft);
  box-shadow: 0 10px 30px -12px rgb(15 23 42 / 0.12);
}
html.dark .nav-inner { box-shadow: 0 8px 30px -14px rgb(0 0 0 / 0.5); }

.mobile-panel {
  pointer-events: auto;
  width: min(1200px, calc(100% - 32px));
  margin: 6px auto 0;
  border-radius: 1rem;
  background: var(--menu-bg);
  -webkit-backdrop-filter: blur(18px);
  backdrop-filter: blur(18px);
  border: 1px solid var(--ink-soft);
  box-shadow: var(--glass-shadow-lg);
}

.top-link {
  padding: 0.55rem 1.1rem;
  border-radius: 999px;
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-2);
  background: transparent;
  border: none;
  cursor: pointer;
  transition: color 0.2s ease, background 0.2s ease, transform 0.15s ease;
}
.top-link:active { transform: scale(0.95); }
.top-link:hover { color: var(--fg); background: var(--hover); }
.home-bg .top-link.on { color: #18181b; background: rgba(24, 24, 27, 0.07); }
.top-link.on {
  color: var(--m-orange);
  font-weight: 700;
  background: rgb(24 24 27 / 0.12);
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
.m-link:hover { color: var(--fg); background: var(--hover); border-color: rgb(24 24 27 / 0.3); }
.m-link.m-on {
  color: #fff;
  font-weight: 700;
  background: linear-gradient(120deg, #18181b, #27272a);
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
.round-btn:hover { color: var(--fg); background: var(--hover); border-color: rgb(24 24 27 / 0.4); box-shadow: 0 4px 14px rgb(24 24 27 / 0.16); transform: translateY(-1px); }
/* 首页整页淡紫渐变底（铺满整个页面高度） */
.home-bg {
  background: #fafaf8 !important;
}
/* 游客登录入口：vivid 同款渐变胶囊 */
.login-pill {
  display: inline-flex; align-items: center;
  padding: 8px 18px; border: none; border-radius: 999px;
  background: #18181b;
  color: #fff; font-family: inherit; font-size: 13px; font-weight: 700;
  cursor: pointer; box-shadow: 0 8px 20px -8px rgba(9, 9, 11, 0.5);
  transition: transform 0.15s ease, filter 0.2s ease;
}
.login-pill:hover { transform: translateY(-1px); filter: brightness(1.08); }
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
  background: linear-gradient(120deg, #18181b, #27272a 55%, #52525b 140%);
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
  gap: 0.5rem;
  max-width: 12rem;
  height: 2.5rem;
  padding: 0 0.7rem 0 0.3rem;
  border-radius: 999px;
  border: 1px solid rgb(24 24 27 / 0.1);
  cursor: pointer;
  font-size: 13px;
  font-weight: 700;
  color: var(--fg);
  background: #fff;
  transition: background 0.15s ease, box-shadow 0.15s ease, border-color 0.15s ease;
}
.name-btn:hover, .name-btn.open {
  border-color: rgb(24 24 27 / 0.28);
  box-shadow: 0 4px 14px rgb(24 24 27 / 0.1);
}
.name-txt {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.avatar {
  flex-shrink: 0;
  width: 1.9rem;
  height: 1.9rem;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: #18181b;
  color: #fff;
  font-size: 13px;
  font-weight: 800;
}
.avatar--lg { width: 2.6rem; height: 2.6rem; font-size: 16px; }

.menu {
  position: absolute;
  right: 0;
  top: 3.1rem;
  width: 16rem;
  padding: 0.5rem;
  border-radius: 1rem;
  background: #fff;
  border: 1px solid rgb(24 24 27 / 0.08);
  box-shadow: 0 2px 6px rgb(24 24 27 / 0.05), 0 24px 48px -12px rgb(24 24 27 / 0.22);
  z-index: 50;
}
.menu-head {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.5rem 0.5rem 0.65rem;
}
.menu-name {
  font-size: 14px;
  font-weight: 800;
  color: var(--fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.menu-mail {
  margin-top: 1px;
  font-size: 12px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.menu-balance {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin: 0 0 0.35rem;
  padding: 0.7rem 0.8rem;
  border-radius: 0.75rem;
  background: #f4f4f5;
  text-decoration: none;
  transition: background 0.15s ease;
}
.menu-balance:hover { background: #ececee; }
.mb-label { font-size: 12px; font-weight: 600; color: var(--fg-3); }
.mb-val { font-size: 20px; font-weight: 800; color: var(--fg); font-variant-numeric: tabular-nums; letter-spacing: -0.02em; }
.mb-go { margin-left: auto; font-size: 12px; font-weight: 700; color: var(--fg-2); }
.menu-item {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.55rem 0.7rem;
  border-radius: 0.6rem;
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-2);
  background: transparent;
  border: none;
  cursor: pointer;
  text-decoration: none;
}
.menu-item:hover { background: rgb(24 24 27 / 0.06); color: var(--fg); }
.menu-item--out { color: #dc2626; }
.menu-item--out:hover { background: rgb(220 38 38 / 0.08); color: #dc2626; }
.menu-sep { height: 1px; background: var(--hairline); margin: 0.35rem 0.25rem; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease; }
.fade-enter-from { opacity: 0; }
.fade-leave-to { opacity: 0; }
</style>
