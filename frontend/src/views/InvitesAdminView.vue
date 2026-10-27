<script setup>
// Admin invite log — global view of every invite relationship across accounts.
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { fmtIso } from '../utils/format'
import Icon from '../components/Icon.vue'

const items = ref([])
const stats = ref({ total: 0, completed: 0, pending: 0, reward_paid: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  const r = await api('/invites')
  loading.value = false
  if (r.ok) { items.value = r.data?.data || []; stats.value = r.data?.stats || stats.value }
}
onMounted(load)
</script>

<template>
  <section class="space-y-4">
    <div class="page-head">
      <div>
        <h2 class="page-title">邀请日志</h2>
        <p class="page-sub">全站邀请记录:谁邀请了谁、注册与完成时间、奖励状态</p>
      </div>
      <div class="page-actions">
        <button @click="load" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>
    <!-- KPI strip — same shape as the LogsView so the admin shell stays
         consistent. /invites already returns the stats payload. -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-white/45">总邀请</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums">{{ stats.total }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-emerald-300/80">已完成</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums text-emerald-300">{{ stats.completed }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-amber-300/80">待生图</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums text-amber-300">{{ stats.pending }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-rose-300/80">已发奖励</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums text-rose-300">{{ Number(stats.reward_paid || 0).toLocaleString('en-US') }}</div>
      </div>
    </div>

    <div class="card overflow-hidden">
      
      <div v-if="loading && !items.length" class="empty"><span class="empty-text">加载中…</span></div>
      <div v-else-if="!items.length" class="empty">
        <span class="empty-ico"><Icon name="team" class="w-6 h-6" /></span>
        <span class="empty-text">还没有邀请记录</span>
      </div>

      <table v-else class="w-full text-sm">
        <colgroup>
          <col />
          <col />
          <col class="w-24" />
          <col class="w-44" />
          <col class="w-44" />
          <col class="w-28" />
        </colgroup>
        <thead>
          <tr class="text-[10px] uppercase tracking-[0.2em] text-white/40 border-b border-white/[0.06]">
            <th class="text-left px-5 py-3 font-medium">邀请人</th>
            <th class="text-left px-3 py-3 font-medium">被邀请人</th>
            <th class="text-right px-3 py-3 font-medium">奖励</th>
            <th class="text-left px-3 py-3 font-medium">注册时间</th>
            <th class="text-left px-3 py-3 font-medium">完成时间</th>
            <th class="text-right px-5 py-3 font-medium">状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in items" :key="i"
              class="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors">
            <td class="px-5 py-3.5 align-middle font-medium text-white/90 truncate">{{ r.inviter }}</td>
            <td class="px-3 py-3.5 align-middle text-white/75 truncate">{{ r.invitee }}</td>
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
    </div>
  </section>
</template>
