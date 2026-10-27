<script setup>
// 邀请好友 — its own page (moved out of 设置). Invite data is real, from the
// logged-in account (auth.user). The inviter earns INVITE_REWARD 积分 once each
// invited friend completes their FIRST generation (生图).
import { ref, computed, onMounted } from 'vue'
import { auth, refreshMe } from '../auth'
import { api } from '../api'
import { fmtIso } from '../utils/format'
import Icon from '../components/Icon.vue'

// Reward per completed invite — comes from the backend (credits.invite_reward),
// falling back to 3 until the response lands.
const INVITE_REWARD = ref(3)

const records = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 10
async function loadRecords() {
  loading.value = true
  const r = await api('/auth/invites')
  loading.value = false
  if (r.ok) {
    records.value = r.data?.data || []
    if (r.data?.reward != null) INVITE_REWARD.value = Number(r.data.reward)
    if ((page.value - 1) * pageSize >= records.value.length) page.value = 1
  }
}

// Client-side numbered pagination — matches the admin 日志/图片管理 .pg strip.
const totalPages = computed(() => Math.max(1, Math.ceil(records.value.length / pageSize)))
const paged = computed(() => records.value.slice((page.value - 1) * pageSize, page.value * pageSize))
function goPage(n) { page.value = Math.max(1, Math.min(totalPages.value, n)) }
const pageNumbers = computed(() => {
  const n = totalPages.value
  const cur = page.value
  if (n <= 7) return Array.from({ length: n }, (_, i) => i + 1)
  const want = new Set([1, n, cur - 1, cur, cur + 1])
  if (cur <= 3) { want.add(2); want.add(3); want.add(4) }
  if (cur >= n - 2) { want.add(n - 1); want.add(n - 2); want.add(n - 3) }
  const list = [...want].filter((x) => x >= 1 && x <= n).sort((a, b) => a - b)
  const out = []
  for (let i = 0; i < list.length; i++) {
    if (i > 0 && list[i] - list[i - 1] > 1) out.push(null)
    out.push(list[i])
  }
  return out
})

onMounted(async () => {
  await refreshMe()   // latest invite_count / invite_earned
  loadRecords()
})

const inviteCode = computed(() => auth.user?.invite_code || '')
const inviteCount = computed(() => Number(auth.user?.invite_count || 0))
const inviteEarned = computed(() => Number(auth.user?.invite_earned || 0))
const inviteUrl = computed(() => `${location.origin}/?ref=${inviteCode.value}`)

async function copyInvite() {
  try { await navigator.clipboard.writeText(inviteUrl.value); toast('邀请链接已复制') }
  catch { toast('复制失败') }
}
async function copyCode() {
  try { await navigator.clipboard.writeText(inviteCode.value); toast('邀请码已复制') }
  catch { toast('复制失败') }
}

// ---- Toast ----
const toastMsg = ref('')
let toastTimer = null
function toast(m) {
  toastMsg.value = m
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toastMsg.value = ''), 2200)
}
</script>

<template>
  <div class="theme-text space-y-5">
    <!-- header -->
    <div class="page-head">
      <div>
        <h2 class="page-title">邀请好友</h2>
        <p class="page-sub">好友用你的链接注册，并完成首次生图后，你得 {{ INVITE_REWARD }} 积分。</p>
      </div>
      <div class="page-actions">
        <button @click="loadRecords" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <!-- invite card -->
    <section class="inv-card">
      <div class="inv-main">
        <div class="inv-label">你的专属邀请链接</div>
        <div class="inv-link">
          <span class="inv-url">{{ inviteUrl }}</span>
          <button @click="copyInvite" class="inv-copy"><Icon name="copy" :size="15" /> 复制链接</button>
        </div>
        <button @click="copyCode" class="inv-code" title="点击复制邀请码">
          <span class="inv-code-k">邀请码</span>
          <span class="inv-code-v">{{ inviteCode || '—' }}</span>
          <Icon name="copy" :size="14" />
        </button>
      </div>
      <div class="inv-stats">
        <div class="inv-stat">
          <div class="inv-num">{{ inviteCount }}</div>
          <div class="inv-k">已邀请</div>
        </div>
        <div class="inv-stat">
          <div class="inv-num">{{ inviteEarned.toLocaleString('en-US') }}</div>
          <div class="inv-k">累计积分</div>
        </div>
      </div>
    </section>

    <!-- steps -->
    <section class="steps">
      <div class="step">
        <span class="step-n">1</span>
        <div><div class="step-t">分享链接</div><div class="step-d">把专属链接或邀请码发给好友</div></div>
      </div>
      <div class="step">
        <span class="step-n">2</span>
        <div><div class="step-t">好友注册</div><div class="step-d">好友通过你的链接完成注册</div></div>
      </div>
      <div class="step">
        <span class="step-n">3</span>
        <div><div class="step-t">首次生图，你得 {{ INVITE_REWARD }} 积分</div><div class="step-d">好友完成第一张生图后自动到账</div></div>
      </div>
    </section>

    <!-- records -->
    <section>
      <h3 class="rec-title">邀请记录</h3>

      <div class="card overflow-hidden">
        <div v-if="loading && !records.length" class="text-center text-sm text-white/40 py-16">加载中…</div>
        <div v-else-if="!records.length" class="relative overflow-hidden flex flex-col items-center gap-3 text-white/40 py-14">
          <div class="anime-stars"></div>
          <span class="empty-badge">✦</span>
          <span class="text-sm">还没有人通过你的链接注册～快去分享吧</span>
        </div>

        <table v-else class="w-full text-sm">
          <colgroup>
            <col />
            <col class="w-24" />
            <col class="w-44" />
            <col class="w-44" />
            <col class="w-28" />
          </colgroup>
          <thead>
            <tr class="text-[10px] uppercase tracking-[0.2em] text-white/40 border-b border-white/[0.06]">
              <th class="text-left px-5 py-3 font-medium">注册用户名</th>
              <th class="text-right px-3 py-3 font-medium">奖励</th>
              <th class="text-left px-3 py-3 font-medium">注册时间</th>
              <th class="text-left px-3 py-3 font-medium">完成时间</th>
              <th class="text-right px-5 py-3 font-medium">状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in paged" :key="i"
                class="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors">
              <td class="px-5 py-3.5 align-middle font-medium text-white/90 truncate">{{ r.name }}</td>
              <td class="px-3 py-3.5 align-middle text-right tabular-nums whitespace-nowrap"
                  :class="r.reward ? 'text-emerald-300' : 'text-white/25'">
                {{ r.reward ? '+' + r.reward : '—' }}
              </td>
              <td class="px-3 py-3.5 align-middle text-xs text-white/55 tabular-nums whitespace-nowrap">
                {{ r.registered_at ? fmtIso(r.registered_at) : '—' }}
              </td>
              <td class="px-3 py-3.5 align-middle text-xs text-white/55 tabular-nums whitespace-nowrap">
                {{ r.completed_at ? fmtIso(r.completed_at) : '—' }}
              </td>
              <td class="px-5 py-3.5 align-middle text-right">
                <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium ring-1"
                      :class="r.status === 'completed'
                        ? 'bg-emerald-500/10 text-emerald-300 ring-emerald-400/30'
                        : 'bg-amber-500/10 text-amber-300 ring-amber-400/30'">
                  <span class="w-1.5 h-1.5 rounded-full"
                        :class="r.status === 'completed' ? 'bg-emerald-400' : 'bg-amber-400'"></span>
                  {{ r.status === 'completed' ? '已完成' : '待生图' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>

        <!-- pagination — numbered with ellipsis, same as the admin pages -->
        <div v-if="records.length && totalPages > 1"
             class="flex items-center justify-between gap-3 border-t border-white/[0.06] px-5 py-3 text-xs text-white/55">
          <div>
            <span class="tabular-nums text-white/85">{{ (page - 1) * pageSize + 1 }}–{{ Math.min(records.length, page * pageSize) }}</span>
            <span class="ml-1">/ {{ records.length }} 条</span>
          </div>
          <div class="flex items-center gap-1">
            <template v-for="(n, i) in pageNumbers" :key="i">
              <span v-if="n === null" class="px-1 text-white/35">…</span>
              <button v-else @click="goPage(n)" class="pg" :class="page === n && 'pg-on'">{{ n }}</button>
            </template>
          </div>
        </div>
      </div>
    </section>

    <!-- toast -->
    <transition name="fade">
      <div v-if="toastMsg"
           class="fixed bottom-8 left-1/2 -translate-x-1/2 z-50 bg-white text-black text-sm font-medium px-5 py-2.5 rounded-full shadow-2xl">
        {{ toastMsg }}
      </div>
    </transition>
  </div>
</template>

<style scoped>
.inv-card {
  display: flex; align-items: stretch; justify-content: space-between; gap: 28px; flex-wrap: wrap;
  padding: 28px 30px; border-radius: 22px; position: relative; overflow: hidden;
  background: radial-gradient(520px 260px at 100% 0%, rgba(251, 146, 60, 0.18), transparent 70%), linear-gradient(135deg, #27272a, #09090b);
  box-shadow: 0 20px 44px -20px rgba(9, 9, 11, 0.55);
}
.inv-main { flex: 1 1 420px; min-width: 0; }
.inv-label { font-size: 12.5px; font-weight: 700; color: #a1a1aa; letter-spacing: .04em; }
.inv-link { margin-top: 12px; display: flex; align-items: center; gap: 8px; padding: 6px 6px 6px 16px; border-radius: 14px; background: rgba(255,255,255,.08); border: 1px solid rgba(255,255,255,.12); }
.inv-url { flex: 1; min-width: 0; font-family: ui-monospace, 'SF Mono', Consolas, monospace; font-size: 13px; color: #e4e4e7; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.inv-copy { flex-shrink: 0; display: inline-flex; align-items: center; gap: 6px; height: 38px; padding: 0 16px; border: 0; border-radius: 10px; background: #fff; color: #18181b; font-size: 13px; font-weight: 800; cursor: pointer; transition: transform .12s ease, background .15s ease; }
.inv-copy:hover { background: #f4f4f5; transform: translateY(-1px); }
.inv-code { margin-top: 12px; display: inline-flex; align-items: center; gap: 10px; padding: 8px 14px; border-radius: 10px; border: 1px dashed rgba(255,255,255,.28); background: transparent; cursor: pointer; color: #a1a1aa; transition: border-color .15s ease, color .15s ease; }
.inv-code:hover { border-color: rgba(255,255,255,.55); color: #fff; }
.inv-code-k { font-size: 12px; font-weight: 700; }
.inv-code-v { font-family: ui-monospace, 'SF Mono', Consolas, monospace; font-size: 14px; font-weight: 700; color: #fff; letter-spacing: .06em; }
.inv-stats { display: flex; align-items: center; gap: 34px; padding-left: 8px; }
.inv-stat + .inv-stat { padding-left: 34px; border-left: 1px solid rgba(255,255,255,.14); }
.inv-num { font-size: 40px; line-height: 1; font-weight: 800; letter-spacing: -0.04em; color: #fff; font-variant-numeric: tabular-nums; }
.inv-k { margin-top: 8px; font-size: 12.5px; font-weight: 600; color: #a1a1aa; }

.steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.step { display: flex; align-items: center; gap: 14px; padding: 16px 18px; background: #fff; border: 1px solid rgb(24 24 27 / .07); border-radius: 16px; }
.step-n { flex-shrink: 0; width: 32px; height: 32px; border-radius: 50%; display: grid; place-items: center; background: #f4f4f5; color: #18181b; font-size: 14px; font-weight: 800; }
.step-t { font-size: 14px; font-weight: 800; color: var(--fg); }
.step-d { margin-top: 2px; font-size: 12px; color: var(--fg-3); }
.rec-title { margin: 10px 2px 12px; font-size: 16px; font-weight: 800; letter-spacing: -0.02em; color: var(--fg); }
@media (max-width: 760px) { .steps { grid-template-columns: 1fr; } .inv-stats { padding-left: 0; } }

/* Numbered pagination buttons — dark base (matches the admin pages); the global
   .theme-text rules in style.css recolor these for light mode automatically. */
.pg {
  min-width: 1.75rem;
  padding: 0.3rem 0.55rem;
  font-size: 0.72rem;
  font-weight: 500;
  text-align: center;
  border-radius: 0.45rem;
  color: rgb(255 255 255 / 0.7);
  background: rgb(255 255 255 / 0.04);
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.08);
  transition: background 0.15s, color 0.15s, box-shadow 0.15s;
}
.pg:hover:not(.pg-on) { background: rgb(255 255 255 / 0.1); color: white; }
.pg-on {
  background: rgb(255 255 255 / 0.92);
  color: rgb(15 23 42);
  box-shadow: none;
}
</style>
