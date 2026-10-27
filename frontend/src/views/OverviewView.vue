<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { fmtTs, fmtSize } from '../utils/format'
import Icon from '../components/Icon.vue'

const providers = ref([])
const stats = ref({ generated_count: 0, generated_size_bytes: 0 })
const userStats = ref({ total: 0, active: 0, disabled: 0, admins: 0, credits_total: 0, new_24h: 0, new_7d: 0, active_24h: 0 })
// Everything time-windowed now comes pre-aggregated from /dashboard (server-side
// SQL) instead of being recomputed in the browser from the last 200 logs — which
// silently undercounted week / DAU / trend / top-N once volume grew past 200.
const dash = ref(null)
const logs = ref([])        // recent-activity feed only (small page)
const models = ref([])      // managed models, for the model-count card
const range = ref('day')    // analytics window toggle: 'day' (24h) | 'week' (7d)
let timer = null

async function refreshAll() {
  const [p, s, u, d, l, m] = await Promise.all([
    api('/providers'),
    api('/stats'),
    api('/users'),
    api('/dashboard'),
    api('/logs?limit=20'),
    api('/managed-models'),
  ])
  providers.value = p.data?.data || []
  stats.value = s.data || {}
  userStats.value = u.data?.stats || {}
  dash.value = d.data || null
  logs.value = l.data?.data || []
  models.value = m.data?.data || []
}

// ---- windowed event aggregates (from /dashboard) ----
const EMPTY_WINDOW = { total: 0, success: 0, failed: 0, pending: 0, image: 0, video: 0, api: 0, web: 0, spent: 0 }
const today = computed(() => dash.value?.today || EMPTY_WINDOW)
const todayDau = computed(() => dash.value?.today_dau || 0)
const day = computed(() => dash.value?.day || EMPTY_WINDOW)
const week = computed(() => dash.value?.week || EMPTY_WINDOW)
// All-time persistent counters (stat_counters) — independent of log retention.
const lifetime = computed(() => dash.value?.lifetime || {})
const successRate = computed(() => (day.value.total ? Math.round((day.value.success / day.value.total) * 100) : 0))

// Direction vs the previous 24h (24–48h ago) — a quiet day after a busy week is
// worth seeing. prev_day_total is computed server-side.
const dayDelta = computed(() => {
  const cur = day.value.total
  const prev = dash.value?.prev_day_total || 0
  if (!prev) return cur ? { pct: null, dir: 'up' } : { pct: null, dir: 'flat' }
  const pct = Math.round(((cur - prev) / prev) * 100)
  return { pct, dir: cur > prev ? 'up' : cur < prev ? 'down' : 'flat' }
})

const dau = computed(() => dash.value?.dau || 0)
const avg24hMs = computed(() => stats.value?.avg_elapsed_ms_24h ?? null)

// ---- range-toggled top-N analytics (both windows ship in the payload, so the
// 24h/7d switch is instant — no re-fetch) ----
const rangeLabel = computed(() => (range.value === 'week' ? '近 3 天' : '近 24h'))
const analytics = computed(() => dash.value?.analytics?.[range.value] || { models: [], failures: [], top_users: [] })
const modelUsage = computed(() => analytics.value.models || [])
const usageMax = computed(() => Math.max(1, ...modelUsage.value.map((m) => m.count)))
const failures = computed(() => analytics.value.failures || [])
const topUsers = computed(() => analytics.value.top_users || [])
const topUserMax = computed(() => Math.max(1, ...topUsers.value.map((u) => u.spent)))

// ---- 24h trend (always 24h) ----
const hourBuckets = computed(() => dash.value?.hourly || Array.from({ length: 24 }, () => ({ image: 0, video: 0 })))
const hourMax = computed(() => Math.max(1, ...hourBuckets.value.map((b) => b.image + b.video)))

// ---- operations cards ----
const cdk = computed(() => dash.value?.cdk || {})
const invites = computed(() => dash.value?.invites || {})
const checkin = computed(() => dash.value?.checkin || {})

// ---- channel / model summaries (from /providers — now a channel summary — + /managed-models) ----
const tokens = computed(() => {
  let active = 0, total = 0
  for (const p of providers.value) {
    total += 1
    if (p.enabled) active += 1
  }
  return { active, total }
})

const modelTypes = computed(() => {
  const image = models.value.filter((m) => m.type === 'image').length
  const video = models.value.filter((m) => m.type === 'video').length
  return { image, video, total: models.value.length }
})

const providerHealth = computed(() =>
  providers.value.map((p) => {
    let status = 'down'
    if (p.enabled && (p.model_count || 0) > 0) status = 'healthy'
    else if (p.enabled) status = 'warning'
    return { ...p, status }
  })
)

// One-glance system health badge derived from channel availability.
const overallHealth = computed(() => {
  const list = providerHealth.value
  if (!list.length) return { label: '未配置渠道', tone: 'down' }
  if (list.some((p) => p.status === 'down')) return { label: '渠道停用', tone: 'down' }
  if (list.some((p) => p.status === 'warning')) return { label: '渠道未绑定模型', tone: 'warning' }
  return { label: '系统健康', tone: 'healthy' }
})

const recentLogs = computed(() => logs.value.slice(0, 12))

// ---- formatters ----
function statusLabel(s) { return s === 'healthy' ? '健康' : s === 'warning' ? '告警' : '未配置' }
function statusDot(s) { return s === 'healthy' ? 'bg-emerald-500' : s === 'warning' ? 'bg-amber-500' : 'bg-rose-500' }
function statusPill(s) {
  if (s === 'healthy') return 'bg-emerald-500/10 text-emerald-600 ring-emerald-500/25'
  if (s === 'warning') return 'bg-amber-500/10 text-amber-600 ring-amber-500/25'
  return 'bg-rose-500/10 text-rose-600 ring-rose-500/25'
}
function logDot(status) {
  if (status === 'success') return 'bg-emerald-500'
  if (status === 'pending') return 'bg-amber-500'
  return 'bg-rose-500'
}
function fmtMs(ms) {
  if (!ms) return '—'
  if (ms < 1000) return ms + 'ms'
  return (ms / 1000).toFixed(1) + 's'
}
function fmtInt(n) { return (n ?? 0).toLocaleString('zh-CN') }
function fmtCredits(n) {
  const v = Number(n || 0)
  if (v >= 10000) return (v / 10000).toFixed(1) + ' 万'
  return fmtInt(Math.round(v))
}

onMounted(() => {
  refreshAll()
  timer = setInterval(refreshAll, 10000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <section class="ov">
    <!-- ===== 顶部：健康状态 + 刷新 ===== -->
    <div class="ov-bar">
      <div>
        <h2 class="ov-title">运营概览</h2>
        <p class="ov-sub">数据每 10 秒自动刷新</p>
      </div>
      <div class="ov-bar__right">
        <span class="pill" :class="'pill--' + overallHealth.tone">
          <span class="dot" :class="'dot--' + overallHealth.tone"></span>{{ overallHealth.label }}
        </span>
        <button @click="refreshAll" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <!-- ===== KPI 带：一张卡四列 ===== -->
    <div class="panel kpis">
      <div class="kpi">
        <div class="kpi-label">今日生成</div>
        <div class="kpi-num">{{ fmtInt(today.total) }}</div>
        <div class="kpi-meta">
          <span class="ok">{{ today.success }} 成功</span>
          <span v-if="today.failed" class="bad">{{ today.failed }} 失败</span>
          <span v-if="today.pending" class="warn">{{ today.pending }} 进行中</span>
        </div>
        <div class="kpi-foot">消耗 {{ fmtCredits(today.spent) }} 积分 · {{ fmtInt(todayDau) }} 人活跃</div>
      </div>
      <div class="kpi">
        <div class="kpi-label">近 24 小时</div>
        <div class="kpi-num">
          {{ fmtInt(day.total) }}
          <span v-if="dayDelta.pct != null" class="delta" :class="'delta--' + dayDelta.dir">
            {{ dayDelta.dir === 'up' ? '↑' : dayDelta.dir === 'down' ? '↓' : '·' }}{{ Math.abs(dayDelta.pct) }}%
          </span>
        </div>
        <div class="kpi-meta">
          <span class="ok">{{ day.success }} 成功</span>
          <span v-if="day.failed" class="bad">{{ day.failed }} 失败</span>
          <span class="mute">成功率 {{ successRate }}%</span>
        </div>
        <div class="kpi-foot">Web {{ day.web }} · API {{ day.api }} · 图 {{ day.image }} · 视 {{ day.video }}</div>
      </div>
      <div class="kpi">
        <div class="kpi-label">累计生成</div>
        <div class="kpi-num">{{ fmtInt(lifetime.total || 0) }}</div>
        <div class="kpi-meta">
          <span class="ok">{{ lifetime.success || 0 }} 成功</span>
          <span v-if="lifetime.failed" class="bad">{{ lifetime.failed }} 失败</span>
        </div>
        <div class="kpi-foot">API {{ lifetime.api || 0 }} · 图 {{ lifetime.image || 0 }} · 视 {{ lifetime.video || 0 }}</div>
      </div>
      <div class="kpi">
        <div class="kpi-label">平均耗时 · 24h</div>
        <div class="kpi-num">{{ fmtMs(avg24hMs) }}</div>
        <div class="kpi-meta"><span class="mute">产物存储 {{ fmtSize(stats.generated_size_bytes || 0) }}</span></div>
        <div class="kpi-foot">{{ fmtInt(stats.generated_count) }} 个文件</div>
      </div>
    </div>

    <!-- ===== 资源条：用户 / 渠道 / 模型 / 积分 ===== -->
    <div class="strip">
      <div class="chip">
        <span class="chip-ic"><Icon name="accounts" :size="16" /></span>
        <div><div class="chip-num">{{ fmtInt(userStats.total) }}</div>
          <div class="chip-label">用户 · {{ userStats.active }} 活跃 · 今日 +{{ fmtInt(userStats.new_24h) }}</div></div>
      </div>
      <div class="chip">
        <span class="chip-ic"><Icon name="plug" :size="16" /></span>
        <div><div class="chip-num">{{ tokens.active }}<small> / {{ tokens.total }}</small></div>
          <div class="chip-label">渠道 · 启用 / 已配置</div></div>
      </div>
      <div class="chip">
        <span class="chip-ic"><Icon name="models" :size="16" /></span>
        <div><div class="chip-num">{{ modelTypes.total }}</div>
          <div class="chip-label">模型 · 图像 {{ modelTypes.image }} · 视频 {{ modelTypes.video }}</div></div>
      </div>
      <div class="chip">
        <span class="chip-ic"><Icon name="receipt" :size="16" /></span>
        <div><div class="chip-num">{{ fmtCredits(userStats.credits_total) }}</div>
          <div class="chip-label">系统积分总和 · 24h 消耗 {{ fmtCredits(day.spent) }}</div></div>
      </div>
    </div>

    <!-- ===== 趋势（宽）+ 渠道健康 ===== -->
    <div class="split">
      <div class="panel">
        <div class="panel-h">
          <h3>24 小时生成趋势</h3>
          <div class="legend">
            <span><i class="sw sw--ink"></i>图像</span>
            <span><i class="sw sw--soft"></i>视频</span>
            <span class="tnum">峰值 {{ hourMax }}/h</span>
          </div>
        </div>
        <div class="trend">
          <div class="trend-bars">
            <div v-for="(b, i) in hourBuckets" :key="i" class="tb"
                 :style="{ height: Math.max(4, ((b.image + b.video) / hourMax) * 100) + '%' }">
              <div class="tip">{{ 23 - i }}h 前 · 图 {{ b.image }} / 视 {{ b.video }}</div>
              <div v-if="b.video" class="tb-v" :style="{ flex: b.video }"></div>
              <div v-if="b.image" class="tb-i" :style="{ flex: b.image }"></div>
              <div v-if="!b.image && !b.video" class="tb-0"></div>
            </div>
          </div>
          <div class="trend-x"><span>-24h</span><span>-18h</span><span>-12h</span><span>-6h</span><span>现在</span></div>
        </div>
      </div>

      <div class="panel">
        <div class="panel-h"><h3>渠道健康</h3></div>
        <div class="list">
          <div v-if="!providerHealth.length" class="empty">未配置渠道</div>
          <div v-for="p in providerHealth" :key="p.id" class="row">
            <div class="row-main">
              <div class="row-name">{{ p.name }}</div>
              <div class="row-sub">{{ p.model_count }} 模型 · 成功 {{ p.success_total || 0 }}<span v-if="p.upstream_balance != null"> · 余额 {{ p.upstream_balance }}</span></div>
            </div>
            <span class="pill" :class="'pill--' + p.status"><span class="dot" :class="'dot--' + p.status"></span>{{ statusLabel(p.status) }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- ===== 运营：CDK / 邀请 / 签到 ===== -->
    <div class="trio">
      <div class="panel ops">
        <div class="ops-label">兑换码 CDK</div>
        <div class="ops-num"><span class="ok">{{ fmtInt(cdk.active) }}</span><small>未用</small> <span class="sep">/</span> {{ fmtInt(cdk.redeemed) }}<small>已兑</small></div>
        <div class="ops-foot">待兑 {{ fmtCredits(cdk.active_amount) }} 积分 · 已发出 {{ fmtCredits(cdk.redeemed_amount) }}</div>
      </div>
      <div class="panel ops">
        <div class="ops-label">邀请</div>
        <div class="ops-num">{{ fmtInt(invites.total) }}<small>邀请注册</small></div>
        <div class="ops-foot">{{ fmtInt(invites.completed) }} 已达成奖励 · 已发 {{ fmtCredits(invites.reward_paid) }} 积分</div>
      </div>
      <div class="panel ops">
        <div class="ops-label">今日签到</div>
        <div class="ops-num">{{ fmtInt(checkin.today) }}<small>人</small></div>
        <div class="ops-foot">发放 {{ fmtCredits(checkin.awarded_today) }} 积分</div>
      </div>
    </div>

    <!-- ===== 使用分析 ===== -->
    <div class="ov-bar ov-bar--sub">
      <h2 class="ov-title ov-title--sm">使用分析 <span class="mute">· {{ rangeLabel }}</span></h2>
      <div class="seg">
        <button :class="{ on: range === 'day' }" @click="range = 'day'">近 24h</button>
        <button :class="{ on: range === 'week' }" @click="range = 'week'">近 3d</button>
      </div>
    </div>

    <div class="split split--even">
      <div class="panel">
        <div class="panel-h"><h3>热门模型</h3></div>
        <div class="list list--pad">
          <div v-if="!modelUsage.length" class="empty">尚无生成记录</div>
          <div v-for="m in modelUsage" :key="m.model" class="bar-row">
            <div class="bar-top">
              <span class="mono">{{ m.model }}</span>
              <span class="bar-right"><em v-if="m.avg_ms">{{ fmtMs(m.avg_ms) }}</em><b>{{ m.count }}</b></span>
            </div>
            <div class="bar"><i :style="{ width: ((m.count / usageMax) * 100) + '%' }"></i></div>
          </div>
        </div>
      </div>
      <div class="panel">
        <div class="panel-h"><h3>失败原因 Top</h3></div>
        <div class="list list--pad">
          <div v-if="!failures.length" class="empty">{{ rangeLabel }}内没有失败，一切正常</div>
          <div v-for="f in failures" :key="f.reason" class="fail">
            <span class="dot dot--down"></span>
            <span class="fail-r">{{ f.reason }}</span>
            <b class="bad">×{{ f.count }}</b>
          </div>
        </div>
      </div>
      <div class="panel">
        <div class="panel-h"><h3>用户消耗 Top</h3></div>
        <div class="list list--pad">
          <div v-if="!topUsers.length" class="empty">{{ rangeLabel }}内无消耗记录</div>
          <div v-for="u in topUsers" :key="u.user_id || u.name" class="bar-row">
            <div class="bar-top">
              <span>{{ u.name }}</span>
              <span class="bar-right"><em>{{ u.count }} 次</em><b>{{ fmtCredits(u.spent) }}</b></span>
            </div>
            <div class="bar"><i :style="{ width: ((u.spent / topUserMax) * 100) + '%' }"></i></div>
          </div>
        </div>
      </div>
      <div class="panel">
        <div class="panel-h"><h3>积分消耗概览</h3></div>
        <div class="mini">
          <div><div class="mini-l">近 24 小时</div><div class="mini-n">{{ fmtCredits(day.spent) }}</div><div class="mini-s">{{ day.success }} 次成功生成</div></div>
          <div><div class="mini-l">近 3 天</div><div class="mini-n">{{ fmtCredits(week.spent) }}</div><div class="mini-s">{{ week.success }} 次成功生成</div></div>
          <div><div class="mini-l">单次均价</div><div class="mini-n">{{ day.success ? fmtCredits(Math.round(day.spent / day.success)) : '—' }}</div><div class="mini-s">24h 平均</div></div>
          <div><div class="mini-l">消耗用户数</div><div class="mini-n">{{ topUsers.length }}</div><div class="mini-s">{{ rangeLabel }}有消耗</div></div>
        </div>
      </div>
    </div>

    <!-- ===== 最近活动 ===== -->
    <div class="panel">
      <div class="panel-h">
        <h3>最近活动</h3>
        <router-link to="/admin/logs" class="more">查看全部 →</router-link>
      </div>
      <div class="list">
        <div v-if="!recentLogs.length" class="empty">尚无活动</div>
        <div v-for="e in recentLogs" :key="e.id" class="ov-act">
          <span class="dot" :class="e.status === 'success' ? 'dot--healthy' : e.status === 'pending' ? 'dot--warning' : 'dot--down'"></span>
          <span class="ov-act-kind">{{ e.kind === 'video' ? '视频' : '图像' }}</span>
          <span class="mono ov-act-model">{{ e.model }}</span>
          <span class="ov-act-prompt">{{ e.prompt }}</span>
          <span class="ov-act-ms tnum">{{ fmtMs(e.elapsed_ms) }}</span>
          <span class="ov-act-ts tnum">{{ fmtTs(e.ts) }}</span>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.ov { display: flex; flex-direction: column; gap: 16px; color: var(--fg); }
.tnum { font-variant-numeric: tabular-nums; }
.mono { font-family: ui-monospace, 'SF Mono', Consolas, monospace; font-size: 12px; }
.mute { color: var(--fg-3); font-weight: 500; }
.ok { color: #15803d; } .bad { color: #dc2626; } .warn { color: #b45309; }

.ov-bar { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; }
.ov-bar--sub { margin-top: 8px; align-items: center; }
.ov-bar__right { display: flex; align-items: center; gap: 10px; }
.ov-title { font-size: 20px; font-weight: 800; letter-spacing: -0.03em; color: var(--fg); }
.ov-title--sm { font-size: 16px; }
.ov-sub { margin-top: 2px; font-size: 12px; color: var(--fg-3); }
.ov-btn { display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 14px; border-radius: 999px; border: 1px solid rgb(24 24 27 / 0.12); background: var(--surface-solid); font-size: 13px; font-weight: 600; color: var(--fg); cursor: pointer; transition: border-color .15s, box-shadow .15s; }
.ov-btn:hover { border-color: rgb(24 24 27 / 0.3); box-shadow: 0 4px 12px rgb(24 24 27 / 0.08); }

.panel { background: var(--surface-solid); border: 1px solid rgb(24 24 27 / 0.07); border-radius: 18px; box-shadow: 0 1px 2px rgb(24 24 27 / 0.03), 0 10px 28px -16px rgb(24 24 27 / 0.12); }
.panel-h { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px 0; }
.panel-h h3 { font-size: 14px; font-weight: 800; color: var(--fg); letter-spacing: -0.01em; }
.more { font-size: 12px; font-weight: 600; color: var(--fg-3); text-decoration: none; }
.more:hover { color: var(--fg); }

.pill { display: inline-flex; align-items: center; gap: 6px; padding: 5px 11px; border-radius: 999px; font-size: 12px; font-weight: 700; background: #f4f4f5; color: var(--fg-2); }
.pill--healthy { background: rgb(21 128 61 / 0.1); color: #15803d; }
.pill--warning { background: rgb(180 83 9 / 0.1); color: #b45309; }
.pill--down { background: rgb(220 38 38 / 0.1); color: #dc2626; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #a1a1aa; flex-shrink: 0; }
.dot--healthy { background: #22c55e; } .dot--warning { background: #f59e0b; } .dot--down { background: #ef4444; }

.kpis { display: grid; grid-template-columns: repeat(4, 1fr); }
.kpi { padding: 22px 24px; }
.kpi + .kpi { border-left: 1px solid rgb(24 24 27 / 0.07); }
.kpi-label { font-size: 12px; font-weight: 700; color: var(--fg-3); letter-spacing: .02em; }
.kpi-num { margin-top: 10px; font-size: 34px; font-weight: 800; letter-spacing: -0.04em; line-height: 1; color: var(--fg); font-variant-numeric: tabular-nums; display: flex; align-items: baseline; gap: 8px; }
.kpi-meta { margin-top: 12px; display: flex; flex-wrap: wrap; gap: 4px 10px; font-size: 12px; font-weight: 700; }
.kpi-foot { margin-top: 8px; font-size: 11.5px; color: var(--fg-3); }
.delta { font-size: 12px; font-weight: 700; }
.delta--up { color: #15803d; } .delta--down { color: #dc2626; } .delta--flat { color: var(--fg-3); }

.strip { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.chip { display: flex; align-items: center; gap: 12px; padding: 14px 16px; background: var(--surface-solid); border: 1px solid rgb(24 24 27 / 0.07); border-radius: 16px; }
.chip-ic { width: 38px; height: 38px; border-radius: 12px; display: grid; place-items: center; background: #f4f4f5; color: #18181b; flex-shrink: 0; }
.chip-num { font-size: 20px; font-weight: 800; letter-spacing: -0.03em; color: var(--fg); font-variant-numeric: tabular-nums; line-height: 1.1; }
.chip-num small { font-size: 13px; font-weight: 600; color: var(--fg-3); }
.chip-label { margin-top: 3px; font-size: 11.5px; color: var(--fg-3); }

.split { display: grid; grid-template-columns: 1.7fr 1fr; gap: 16px; }
.split--even { grid-template-columns: 1fr 1fr; }

.legend { display: flex; align-items: center; gap: 14px; font-size: 11.5px; color: var(--fg-3); }
.legend span { display: flex; align-items: center; gap: 5px; }
.sw { width: 9px; height: 9px; border-radius: 3px; display: inline-block; }
.sw--ink { background: #18181b; } .sw--soft { background: #d4d4d8; }
.trend { padding: 18px 20px 16px; display: flex; flex-direction: column; height: calc(100% - 44px); min-height: 220px; }
.trend-bars { flex: 1; display: flex; align-items: flex-end; gap: 4px; min-height: 150px; }
.tb { position: relative; flex: 1; display: flex; flex-direction: column; justify-content: flex-end; border-radius: 4px 4px 0 0; overflow: visible; }
.tb-i { background: #18181b; border-radius: 3px 3px 0 0; } .tb-v { background: #d4d4d8; }
.tb-0 { flex: 1; background: #f1f1f3; border-radius: 3px 3px 0 0; }
.tb:hover .tb-i { background: #3f3f46; }
.tip { display: none; position: absolute; top: -34px; left: 50%; transform: translateX(-50%); white-space: nowrap; z-index: 5; background: #18181b; color: #fff; font-size: 10.5px; padding: 4px 8px; border-radius: 6px; font-variant-numeric: tabular-nums; }
.tb:hover .tip { display: block; }
.trend-x { display: flex; justify-content: space-between; margin-top: 8px; font-size: 10.5px; color: var(--fg-3); }

.list { padding: 8px 12px 12px; }
.list--pad { padding: 14px 20px 18px; display: flex; flex-direction: column; gap: 14px; }
.empty { padding: 28px 0; text-align: center; font-size: 12.5px; color: var(--fg-3); }
.row { display: flex; align-items: center; gap: 12px; padding: 10px 8px; border-radius: 12px; }
.row:hover { background: #f7f7f8; }
.row-main { flex: 1; min-width: 0; }
.row-name { font-size: 13.5px; font-weight: 700; color: var(--fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row-sub { margin-top: 2px; font-size: 11.5px; color: var(--fg-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.trio { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.ops { padding: 18px 20px; }
.ops-label { font-size: 12px; font-weight: 700; color: var(--fg-3); }
.ops-num { margin-top: 8px; font-size: 24px; font-weight: 800; letter-spacing: -0.03em; font-variant-numeric: tabular-nums; color: var(--fg); }
.ops-num small { margin-left: 4px; font-size: 12px; font-weight: 600; color: var(--fg-3); letter-spacing: 0; }
.ops-num .sep { color: #d4d4d8; font-weight: 400; }
.ops-foot { margin-top: 6px; font-size: 11.5px; color: var(--fg-3); }

.seg { display: inline-flex; padding: 3px; border-radius: 999px; background: #f0f0f2; }
.seg button { padding: 5px 14px; border: 0; border-radius: 999px; background: transparent; font-size: 12px; font-weight: 700; color: var(--fg-3); cursor: pointer; transition: all .15s; }
.seg button.on { background: var(--surface-solid); color: var(--fg); box-shadow: 0 1px 3px rgb(24 24 27 / 0.12); }

.bar-top { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 6px; font-size: 13px; color: var(--fg); }
.bar-top > span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar-right { display: flex; align-items: baseline; gap: 10px; flex-shrink: 0; }
.bar-right em { font-style: normal; font-size: 11px; color: var(--fg-3); font-variant-numeric: tabular-nums; }
.bar-right b { font-size: 13px; font-weight: 800; font-variant-numeric: tabular-nums; }
.bar { height: 6px; border-radius: 999px; background: #f1f1f3; overflow: hidden; }
.bar i { display: block; height: 100%; border-radius: 999px; background: linear-gradient(90deg, #18181b, #52525b); }

.fail { display: flex; align-items: flex-start; gap: 10px; }
.fail .dot { margin-top: 6px; }
.fail-r { flex: 1; min-width: 0; font-size: 12.5px; line-height: 1.5; color: var(--fg); word-break: break-all; }

.mini { display: grid; grid-template-columns: 1fr 1fr; gap: 20px 24px; padding: 18px 20px 22px; }
.mini-l { font-size: 12px; font-weight: 700; color: var(--fg-3); }
.mini-n { margin-top: 4px; font-size: 24px; font-weight: 800; letter-spacing: -0.03em; font-variant-numeric: tabular-nums; color: var(--fg); }
.mini-s { margin-top: 2px; font-size: 11.5px; color: var(--fg-3); }

.ov-act { display: flex; align-items: center; gap: 12px; padding: 9px 8px; border-radius: 10px; font-size: 12.5px; }
.ov-act:hover { background: #f7f7f8; }
.ov-act-kind { width: 32px; font-size: 11px; font-weight: 700; color: var(--fg-3); }
.ov-act-model { width: 170px; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg); }
.ov-act-prompt { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-2); }
.ov-act-ms { width: 52px; text-align: right; color: var(--fg-3); }
.ov-act-ts { color: var(--fg-3); white-space: nowrap; }

@media (max-width: 1100px) {
  .kpis, .strip { grid-template-columns: repeat(2, 1fr); }
  .kpi:nth-child(3) { border-left: 0; }
  .kpi:nth-child(n+3) { border-top: 1px solid rgb(24 24 27 / 0.07); }
  .split, .split--even, .trio { grid-template-columns: 1fr; }
}
</style>
