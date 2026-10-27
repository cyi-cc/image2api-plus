<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'
import Icon from '../components/Icon.vue'

const items = ref([])
const total = ref(0)
const loading = ref(false)
const search = ref('')
const page = ref(1)
const pageSize = 20

async function load() {
  loading.value = true
  const qs = new URLSearchParams({ limit: String(pageSize), offset: String((page.value - 1) * pageSize) })
  if (search.value.trim()) qs.set('q', search.value.trim())
  const r = await api('/banned-word-hits?' + qs.toString())
  loading.value = false
  if (r.ok) {
    items.value = r.data?.data || []
    total.value = Number(r.data?.total ?? items.value.length)
  }
}
onMounted(load)

function doSearch() { page.value = 1; load() }

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageStart = computed(() => total.value === 0 ? 0 : (page.value - 1) * pageSize + 1)
const pageEnd = computed(() => Math.min(total.value, page.value * pageSize))
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
        <h2 class="page-title">违禁词触发列表</h2>
        <p class="page-sub">{{ total }} 条触发记录 · 每次拦截记一条(违禁词 / 用户 / 时间 / 提示词)</p>
      </div>
      <div class="page-actions">
        <button @click="load" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <div class="card toolbar">
      <div class="flex-1 min-w-[200px]">
        <input v-model="search" @keyup.enter="doSearch" @change="doSearch"
               class="field" placeholder="搜索 违禁词 / 用户名 / 提示词…" />
      </div>
    </div>

    <div class="card overflow-hidden">
      <table class="w-full text-sm table-fixed">
        <colgroup>
          <col class="w-36" />
          <col class="w-44" />
          <col />
          <col class="w-40" />
        </colgroup>
        <thead>
          <tr class="text-[10px] uppercase tracking-[0.2em] text-white/40 border-b border-white/[0.06]">
            <th class="text-left px-5 py-3 font-medium">违禁词</th>
            <th class="text-left px-3 py-3 font-medium">用户</th>
            <th class="text-left px-3 py-3 font-medium">提示词</th>
            <th class="text-left px-3 py-3 font-medium">触发时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading && !items.length"><td colspan="4"><div class="empty"><span class="empty-text">加载中…</span></div></td></tr>
          <tr v-else-if="!items.length"><td colspan="4"><div class="empty">
            <span class="empty-ico"><Icon name="ban" class="w-6 h-6" /></span>
            <span class="empty-text">{{ search.trim() ? '没有匹配的记录' : '还没有触发记录' }}</span>
          </div></td></tr>
          <tr v-for="h in items" :key="h.id" class="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors align-top">
            <td class="px-5 py-3.5 text-sm font-medium text-rose-300">{{ h.word }}</td>
            <td class="px-3 py-3.5">
              <div class="text-sm text-white/85 truncate" :title="h.user_name">{{ h.user_name || '—' }}</div>
            </td>
            <td class="px-3 py-3.5 text-xs text-white/60">
              <div class="line-clamp-2 break-all" :title="h.prompt">{{ h.prompt || '—' }}</div>
            </td>
            <td class="px-3 py-3.5 text-xs text-white/50 tabular-nums">{{ new Date(h.created_at).toLocaleString() }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="totalPages > 1" class="flex items-center justify-between px-5 py-3 border-t border-white/[0.06] text-xs text-white/45">
        <div><span class="tabular-nums text-white/75">{{ pageStart }}–{{ pageEnd }}</span><span class="ml-1">/ {{ total }} 条</span></div>
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
.pg { min-width: 1.75rem; padding: 0.3rem 0.55rem; font-size: 0.72rem; font-weight: 500; text-align: center; border-radius: 0.45rem; color: var(--fg-2); background: var(--surface-2); box-shadow: inset 0 0 0 1px var(--hairline); transition: background 0.15s, color 0.15s; }
.pg:hover:not(.pg-on) { background: var(--hover); color: var(--fg); }
.pg-on { background: var(--surface-solid); color: rgb(15 23 42); box-shadow: none; }
.line-clamp-2 { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
</style>
