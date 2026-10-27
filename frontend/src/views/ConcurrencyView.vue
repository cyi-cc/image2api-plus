<script setup>
import { ref, onMounted } from 'vue'
import { api, jsonBody } from '../api'
import Icon from '../components/Icon.vue'

const items = ref([])
const loading = ref(false)
const toast = ref('')
let toastTimer = null
function flash(msg) { toast.value = msg; clearTimeout(toastTimer); toastTimer = setTimeout(() => (toast.value = ''), 1800) }

async function load() {
  loading.value = true
  const r = await api('/concurrency-groups')
  items.value = r.data?.data || []
  loading.value = false
}

// add / edit modal
const editing = ref(null)   // { id?, name, max_concurrency }
function openNew() { editing.value = { id: '', name: '', max_concurrency: 10 } }
function openEdit(g) { editing.value = { id: g.id, name: g.name, max_concurrency: g.max_concurrency } }
async function save() {
  const e = editing.value
  const body = { name: (e.name || '').trim(), max_concurrency: Math.max(0, Number(e.max_concurrency) || 0) }
  if (!body.name) { flash('名称不能为空'); return }
  const r = e.id
    ? await api(`/concurrency-groups/${e.id}`, jsonBody('PATCH', body))
    : await api('/concurrency-groups', jsonBody('POST', body))
  if (r.ok) { editing.value = null; flash('已保存'); load() }
  else flash(r.data?.detail || '保存失败')
}

async function setDefault(g) {
  if (g.is_default) return
  const r = await api(`/concurrency-groups/${g.id}/default`, jsonBody('POST', {}))
  if (r.ok) { flash('已设为默认注册分组'); load() }
  else flash(r.data?.detail || '操作失败')
}

async function del(g) {
  if (g.is_default) { flash('默认分组不可删除'); return }
  if (!confirm(`删除分组「${g.name}」?其下 ${g.user_count} 个用户将转入默认分组。`)) return
  const r = await api(`/concurrency-groups/${g.id}`, { method: 'DELETE' })
  if (r.ok) { flash('已删除'); load() }
  else flash(r.data?.detail || '删除失败')
}

onMounted(load)
</script>

<template>
  <section class="theme-text space-y-4">
    <div class="page-head">
      <div>
        <h2 class="page-title">并发分组</h2>
        <p class="page-sub">每个分组限制成员用户的<strong>同时生成数</strong>(画图台 + API key 合计)。<strong>0 = 不限制</strong>。新用户自动进入「默认注册分组」。</p>
      </div>
      <div class="page-actions">
        <button @click="load" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <div class="page-bar"><button @click="openNew" class="btn-primary shrink-0"><Icon name="plus" class="w-4 h-4" /> 新增分组</button></div>

    <div class="card overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-[10px] uppercase tracking-[0.2em] text-white/40 border-b border-white/[0.06]">
            <th class="text-left px-5 py-3 font-medium">名称</th>
            <th class="text-right px-3 py-3 font-medium">并发上限</th>
            <th class="text-right px-3 py-3 font-medium">用户数</th>
            <th class="text-left px-3 py-3 font-medium">默认注册</th>
            <th class="text-right px-3 py-3 font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading"><td colspan="5"><div class="empty"><span class="empty-text">加载中…</span></div></td></tr>
          <tr v-else-if="!items.length"><td colspan="5"><div class="empty">
            <span class="empty-ico"><Icon name="layers" class="w-6 h-6" /></span>
            <span class="empty-text">还没有分组</span>
          </div></td></tr>
          <tr v-for="g in items" :key="g.id" class="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors">
            <td class="px-5 py-3.5 align-middle text-sm font-medium text-white/90">{{ g.name }}</td>
            <td class="px-3 py-3.5 align-middle text-right tabular-nums">
              <span v-if="g.max_concurrency > 0" class="text-white/85">{{ g.max_concurrency }}</span>
              <span v-else class="text-emerald-300/90">不限制</span>
            </td>
            <td class="px-3 py-3.5 align-middle text-right tabular-nums text-white/70">{{ g.user_count }}</td>
            <td class="px-3 py-3.5 align-middle">
              <span v-if="g.is_default" class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium bg-rose-500/10 text-rose-300 ring-1 ring-rose-400/30">
                <span class="w-1.5 h-1.5 rounded-full bg-rose-400"></span> 默认
              </span>
              <button v-else @click="setDefault(g)" class="btn-soft text-xs">设为默认</button>
            </td>
            <td class="px-3 py-3.5 align-middle text-right whitespace-nowrap">
              <div class="inline-flex items-center gap-1">
                <button @click="openEdit(g)" class="act" title="编辑"><Icon name="config" class="w-3.5 h-3.5" /></button>
                <button @click="del(g)" :disabled="g.is_default" class="act danger disabled:opacity-30 disabled:cursor-not-allowed" :title="g.is_default ? '默认分组不可删除' : '删除'"><Icon name="trash" class="w-3.5 h-3.5" /></button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- add / edit modal -->
    <div v-if="editing" class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-start justify-center p-4 overflow-y-auto" @click.self="editing = null">
      <div class="card !shadow-2xl my-12 w-full max-w-sm">
        <div class="px-5 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 class="text-sm font-semibold">{{ editing.id ? '编辑分组' : '新增分组' }}</h2>
          <button @click="editing = null" class="text-white/40 hover:text-white"><Icon name="close" class="w-5 h-5" /></button>
        </div>
        <div class="p-5 space-y-3">
          <div>
            <label class="block text-xs text-white/55 mb-1.5">名称</label>
            <input v-model="editing.name" class="field" placeholder="如:VIP 并发 / 试用" />
          </div>
          <div>
            <label class="block text-xs text-white/55 mb-1.5">并发上限 <span class="text-white/35">(0 = 不限制)</span></label>
            <input v-model.number="editing.max_concurrency" type="number" min="0" class="field" />
          </div>
          <div class="flex justify-end gap-2 pt-1">
            <button @click="editing = null" class="btn-soft">取消</button>
            <button @click="save" class="btn-primary">保存</button>
          </div>
        </div>
      </div>
    </div>

    <transition name="fade">
      <div v-if="toast" class="fixed bottom-6 left-1/2 -translate-x-1/2 z-[60] bg-slate-900 text-white text-xs px-4 py-2 rounded-lg shadow-lg">{{ toast }}</div>
    </transition>
  </section>
</template>

<style scoped>
.act {
  display: inline-flex; align-items: center; justify-content: center;
  width: 1.9rem; height: 1.9rem; border-radius: 0.5rem;
  color: var(--fg-2); background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  transition: background 0.15s, color 0.15s;
}
.act:hover { background: var(--hover); color: var(--fg); }
.act.danger { color: rgb(253 164 175); background: rgb(244 63 94 / 0.12); box-shadow: inset 0 0 0 1px rgb(244 63 94 / 0.3); }
.act.danger:hover { color: white; background: rgb(244 63 94 / 0.25); }
.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
