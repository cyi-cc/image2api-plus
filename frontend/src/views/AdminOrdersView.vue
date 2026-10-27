<script setup>
// Admin 订单 page — all recharge orders, dark admin look (filter pills + search +
// numbered pagination), read-only with 用户名.
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'
import Icon from '../components/Icon.vue'
import SelectMenu from '../components/SelectMenu.vue'

const items = ref([])
const total = ref(0)
const loading = ref(false)
const status = ref('')
const source = ref('')
const search = ref('')
const page = ref(1)
const pageSize = 20

const STATUS = { pending: '待支付', paid: '已支付', cancelled: '已取消' }
const METHOD = { wxpay: '微信', alipay: '支付宝', admin: '后台充值', cdk: '兑换码' }
const chipClass = (s) => ({
  paid: 'fp-emerald', pending: 'fp-amber', cancelled: '',
}[s] || '')

function fmt(unix) {
  if (!unix) return '—'
  const d = new Date(unix * 1000)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

const STATUS_OPTS = [{ value: '', label: '全部状态' }, { value: 'pending', label: '待支付' }, { value: 'paid', label: '已支付' }, { value: 'cancelled', label: '已取消' }]
const SOURCE_OPTS = [{ value: '', label: '全部来源' }, { value: 'epay', label: '在线充值' }, { value: 'admin', label: '后台充值' }, { value: 'cdk', label: '兑换码' }]
async function load() {
  loading.value = true
  const qs = new URLSearchParams({ limit: String(pageSize), offset: String((page.value - 1) * pageSize) })
  if (status.value) qs.set('status', status.value)
  if (source.value) qs.set('source', source.value)
  if (search.value.trim()) qs.set('q', search.value.trim())
  const r = await api('/pay/admin/orders?' + qs.toString())
  loading.value = false
  if (r.ok) {
    items.value = r.data?.data || []
    total.value = Number(r.data?.total ?? items.value.length)
  }
}
onMounted(load)

// 搜索走服务端(跨页)，直接展示服务端返回的当页结果。
const displayed = computed(() => items.value)
function doSearch() { page.value = 1; load() }
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageStart = computed(() => total.value === 0 ? 0 : (page.value - 1) * pageSize + 1)
const pageEnd = computed(() => Math.min(total.value, page.value * pageSize))
function setStatus(v) { status.value = v; page.value = 1; load() }
function setSource(v) { source.value = v; page.value = 1; load() }
const pageNumbers = computed(() => {
  const n = totalPages.value, cur = page.value
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
function goPage(n) {
  const t = Math.max(1, Math.min(totalPages.value, n))
  if (t === page.value) return
  page.value = t; load()
}
</script>

<template>
  <section class="theme-text space-y-4">
    <div class="page-head">
      <div>
        <h2 class="page-title">订单管理</h2>
        <p class="page-sub">{{ total }} 笔充值订单</p>
      </div>
      <div class="page-actions">
        <button @click="load" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <div class="card toolbar">
      <SelectMenu :model-value="status" compact :options="STATUS_OPTS" @update:model-value="setStatus" />
      <SelectMenu :model-value="source" compact :options="SOURCE_OPTS" @update:model-value="setSource" />
      <div class="flex-1 min-w-[200px]">
        <input v-model="search" @keyup.enter="doSearch" @change="doSearch"
               class="field" placeholder="搜索 订单号 / 用户名 / 金额…" />
      </div>
    </div>

    <div v-if="loading && !items.length" class="card text-center text-sm text-white/40 py-20">加载中…</div>
    <div v-else-if="!total" class="card text-center text-sm text-white/40 py-20">暂无订单</div>

    <div v-else class="card overflow-x-auto !p-0">
      <table class="w-full text-sm log-table min-w-[820px]">
        <thead>
          <tr class="text-[10px] uppercase tracking-[0.18em] text-white/40 border-b border-white/[0.06]">
            <th class="text-left px-5 py-3 font-medium">订单号</th>
            <th class="text-left px-3 py-3 font-medium">用户名</th>
            <th class="text-left px-3 py-3 font-medium">下单时间</th>
            <th class="text-left px-3 py-3 font-medium">支付时间</th>
            <th class="text-right px-3 py-3 font-medium">金额</th>
            <th class="text-right px-3 py-3 font-medium">充值积分</th>
            <th class="text-left px-3 py-3 font-medium">状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in displayed" :key="o.id" class="log-row">
            <td class="px-5 py-3.5 align-middle font-mono text-xs text-white/80" :title="o.remark">{{ o.id }}<span v-if="o.remark" class="ml-2 font-sans text-[10px] text-white/40">{{ o.remark }}</span></td>
            <td class="px-3 py-3.5 align-middle text-white/85 truncate max-w-[140px]" :title="o.user_name">{{ o.user_name || '—' }}</td>
            <td class="px-3 py-3.5 align-middle text-xs text-white/55 whitespace-nowrap">{{ fmt(o.created_at) }}</td>
            <td class="px-3 py-3.5 align-middle text-xs text-white/55 whitespace-nowrap">{{ fmt(o.paid_at) }}</td>
            <td class="px-3 py-3.5 align-middle text-right tabular-nums text-white/85">{{ o.source && o.source !== 'epay' ? '—' : '¥' + o.amount }}</td>
            <td class="px-3 py-3.5 align-middle text-right tabular-nums" :class="o.points < 0 ? 'text-rose-300' : 'text-orange-300'">{{ o.points }}</td>
            <td class="px-3 py-3.5 align-middle">
              <span class="chip" :class="chipClass(o.status)">{{ STATUS[o.status] }}<span class="opacity-50 ml-1">· {{ METHOD[o.pay_type] || o.pay_type }}</span></span>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-if="total && totalPages > 1"
           class="flex items-center justify-between gap-3 border-t border-white/[0.06] px-5 py-3 text-xs text-white/50">
        <div><span class="tabular-nums text-white/75">{{ pageStart }}–{{ pageEnd }}</span><span class="ml-1">/ {{ total }} 笔</span></div>
        <div class="flex items-center gap-1">
          <template v-for="(n, i) in pageNumbers" :key="i">
            <span v-if="n === null" class="px-1 text-white/30">…</span>
            <button v-else @click="goPage(n)" class="pg" :class="page === n && 'pg-on'">{{ n }}</button>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.fp { display: inline-flex; align-items: center; gap: 0.35rem; padding: 0.35rem 0.7rem; font-size: 0.72rem; border-radius: 0.55rem; color: rgb(255 255 255 / 0.65); background: rgb(255 255 255 / 0.05); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.06); transition: background 0.15s, color 0.15s; }
.fp:hover { background: rgb(255 255 255 / 0.09); color: white; }
.fp-on { background: rgb(255 255 255 / 0.92); color: rgb(15 23 42); box-shadow: none; }
.fp-emerald { background: rgb(16 185 129 / 0.22); color: rgb(110 231 183); box-shadow: inset 0 0 0 1px rgb(110 231 183 / 0.45); }
.fp-amber { background: rgb(245 158 11 / 0.22); color: rgb(252 211 77); box-shadow: inset 0 0 0 1px rgb(252 211 77 / 0.45); }
.chip { display: inline-flex; align-items: center; gap: 0.3rem; padding: 0.18rem 0.55rem; font-size: 0.7rem; font-weight: 500; border-radius: 9999px; white-space: nowrap; background: rgb(255 255 255 / 0.06); color: rgb(255 255 255 / 0.55); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.1); }
.log-table { border-collapse: separate; border-spacing: 0; }
.log-row td { border-bottom: 1px solid rgb(255 255 255 / 0.04); transition: background-color 0.15s ease, box-shadow 0.15s ease; }
.log-row:hover td { background: rgb(255 255 255 / 0.025); }
.log-row:hover td:first-child { box-shadow: inset 2px 0 0 rgb(253 186 116 / 0.55); }
.log-row:last-child td { border-bottom: none; }
.pg { min-width: 1.75rem; padding: 0.3rem 0.55rem; font-size: 0.72rem; font-weight: 500; text-align: center; border-radius: 0.45rem; color: rgb(255 255 255 / 0.7); background: rgb(255 255 255 / 0.04); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.08); transition: background 0.15s, color 0.15s; }
.pg:hover:not(.pg-on) { background: rgb(255 255 255 / 0.1); color: white; }
.pg-on { background: rgb(255 255 255 / 0.92); color: rgb(15 23 42); box-shadow: none; }
</style>
