<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'
import Icon from '../components/Icon.vue'
import SelectMenu from '../components/SelectMenu.vue'
import ModelFormModal from '../components/ModelFormModal.vue'
import TestModal from '../components/TestModal.vue'
import { points } from '../credits'
import { mediaCaps, presetMap } from '../videoCaps'

const models = ref([])
const loading = ref(false)
const showForm = ref(false)
const editing = ref(null) // null = add, object = edit
const testing = ref(null) // model being tested, or null

const kindFilter = ref('')      // '' | 'image' | 'video'
const saleFilter = ref('')      // '' | 'sellable' | 'unsellable'
const KIND_OPTS = [{ value: '', label: '全部类型' }, { value: 'image', label: '图像' }, { value: 'video', label: '视频' }]
const SALE_OPTS = [{ value: '', label: '全部状态' }, { value: 'sellable', label: '已上架' }, { value: 'unsellable', label: '未上架' }]
const search = ref('')

const TYPE_LABEL = { image: '生图', video: '生视频' }
const presets = ref({})   // /video-presets: key → 预设（参考资产分类上限的真源）

function caps(m) { return mediaCaps(m, presets.value[m.id]) }
// 参考图张数:支持音视频参考的模型,max_reference_images 是各类资产合计,图片自己
// 的上限来自预设 max_images。
function refImages(m) { return caps(m)?.images || m.max_reference_images }
const REF_MODE_LABEL = { none: '无', frame: '首帧/首尾帧', asset: '参考图模式' }

function capEmpty(m) {
  if (m.type === 'image') return !(m.ratios || []).length && !m.image_to_image && (m.reference_mode === 'none' || !m.reference_mode)
  return !(m.durations || []).length && !(m.resolutions || []).length && !m.max_reference_images && !caps(m) && !(m.ratios || []).length
}

async function loadModels() {
  loading.value = true
  const [r, p] = await Promise.all([api('/managed-models'), api('/video-presets')])
  models.value = r.data?.data || []
  presets.value = presetMap(p.data?.data)
  loading.value = false
}

function openEdit(m) { editing.value = { ...m }; showForm.value = true }
function onSaved() { showForm.value = false; loadModels() }

async function remove(m) {
  if (!confirm(`确认删除模型 ${m.id}?`)) return
  await api(`/managed-models/${encodeURIComponent(m.id)}`, { method: 'DELETE' })
  loadModels()
}

const stats = computed(() => {
  const total = models.value.length
  const image = models.value.filter((m) => m.type === 'image').length
  const video = models.value.filter((m) => m.type === 'video').length
  const sellable = models.value.filter((m) => m.sellable).length
  return { total, image, video, sellable }
})

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return models.value.filter((m) => {
    if (kindFilter.value && m.type !== kindFilter.value) return false
    if (saleFilter.value === 'sellable' && !m.sellable) return false
    if (saleFilter.value === 'unsellable' && m.sellable) return false
    if (q && !(m.id.toLowerCase().includes(q) || (m.alias || '').toLowerCase().includes(q) || (m.name || '').toLowerCase().includes(q))) return false
    return true
  })
})

onMounted(loadModels)
</script>

<template>
  <section class="space-y-4">
    <div class="page-head">
      <h2 class="page-title">模型管理</h2>
      <button @click="loadModels" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
    </div>
    <!-- KPI strip — same shape as LogsView / InvitesAdminView -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-[color:var(--fg-3)]">总数</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums">{{ stats.total }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-[color:var(--fg-3)]">图像</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums">{{ stats.image }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-[color:var(--fg-3)]">视频</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums">{{ stats.video }}</div>
      </div>
      <div class="card p-4">
        <div class="text-[11px] uppercase tracking-wider text-[color:var(--fg-3)]">上架</div>
        <div class="text-2xl font-semibold mt-1 tabular-nums">{{ stats.sellable }}<span class="text-white/35 text-lg ml-1">/ {{ stats.total }}</span></div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="card toolbar">
      <SelectMenu v-model="kindFilter" compact :options="KIND_OPTS" />
      <SelectMenu v-model="saleFilter" compact :options="SALE_OPTS" />
      <div class="flex-1 min-w-[200px]">
        <input v-model="search" class="field !py-1.5 text-xs" placeholder="搜索 模型 ID / 别名…" />
      </div>
    </div>

    <!-- Table -->
    <div class="card overflow-hidden">
      <div v-if="loading && !models.length" class="empty"><span class="empty-text">加载中…</span></div>
      <div v-else-if="!filtered.length" class="empty">
        <span class="empty-ico"><Icon name="models" class="w-6 h-6" /></span>
        <span v-if="models.length" class="empty-text">没有匹配的模型</span>
        <router-link v-else to="/admin/channels" class="empty-text underline underline-offset-2">还没有模型，先到「渠道管理」添加渠道，保存后会自动同步上游模型</router-link>
      </div>

      <table v-else class="w-full text-sm table-fixed min-w-[1080px]">
        <colgroup>
          <col class="w-52" />     <!-- model name + provider -->
          <col class="w-20" />     <!-- type -->
          <col />                  <!-- pricing -->
          <col />                  <!-- config -->
          <col class="w-24" />     <!-- success count -->
          <col class="w-20" />     <!-- sellable badge -->
          <col class="w-32" />     <!-- actions -->
        </colgroup>
        <thead>
          <tr class="text-[10px] uppercase tracking-[0.2em] text-white/40 border-b border-white/[0.06]">
            <th class="text-left px-5 py-3 font-medium">模型</th>
            <th class="text-left px-3 py-3 font-medium">类型</th>
            <th class="text-left px-3 py-3 font-medium">定价</th>
            <th class="text-left px-3 py-3 font-medium">配置</th>
            <th class="text-right px-3 py-3 font-medium">成功次数</th>
            <th class="text-left px-3 py-3 font-medium">上架</th>
            <th class="text-right px-5 py-3 font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in filtered" :key="m.id"
              class="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors">
            <!-- Model name (mono) + provider / weight subline -->
            <td class="px-5 py-3.5 align-middle min-w-0">
              <div class="font-mono text-[12.5px] text-white/90 truncate" :title="m.id">{{ m.alias || m.name || m.id }}</div>
              <div class="mt-1 text-[10.5px] text-white/45 truncate" :title="(m.channels || []).join('、')">
                {{ (m.channels || []).length ? m.channels.join('、') : (m.provider === 'channel' ? '渠道导入' : (m.provider || '—')) }} · 权重 {{ m.weight || 0 }}
              </div>
            </td>

            <!-- Type tag -->
            <td class="px-3 py-3.5 align-middle">
              <span class="kind-tag" :class="m.type === 'video' ? 'kind-video' : 'kind-image'">
                {{ TYPE_LABEL[m.type] || m.type }}
              </span>
            </td>

            <!-- Pricing — compact chips: 档位 + 普通价 + 代理价 -->
            <td class="px-3 py-3.5 align-middle">
              <div v-if="m.type === 'image'" class="flex flex-wrap gap-1.5">
                <span v-for="r in (m.resolutions || [])" :key="r" class="p-chip">
                  <b>{{ r }}</b><em>{{ points(m.prices?.[r]) }}</em><em class="ag">{{ points(m.prices_agent?.[r] ?? m.prices?.[r]) }}</em>
                </span>
                <span v-if="!(m.resolutions || []).length" class="text-white/30 text-xs">—</span>
              </div>
              <div v-else class="flex flex-wrap gap-1.5">
                <span v-for="r in (m.resolutions || [])" :key="'r'+r" class="p-chip">
                  <b>{{ r }}</b><em>{{ points(m.prices?.[r]) }}</em><em class="ag">{{ points(m.prices_agent?.[r] ?? m.prices?.[r]) }}</em>
                </span>
                <span v-if="m.duration_prices?.per_second != null" class="p-chip">
                  <b>/s</b><em>{{ points(m.duration_prices.per_second) }}</em><em class="ag">{{ points(m.duration_prices_agent?.per_second ?? m.duration_prices.per_second) }}</em>
                </span>
                <span v-else v-for="d in (m.durations || [])" :key="'d'+d" class="p-chip">
                  <b>{{ d }}</b><em>+{{ points(m.duration_prices?.[d]) }}</em><em class="ag">+{{ points(m.duration_prices_agent?.[d] ?? m.duration_prices?.[d]) }}</em>
                </span>
                <span v-if="!(m.resolutions || []).length && !(m.durations || []).length && m.duration_prices?.per_second == null" class="text-white/30 text-xs">—</span>
              </div>
            </td>

            <!-- 配置 — uniform mono tags: duration, resolutions, refs, ratios -->
            <td class="px-3 py-3.5 align-middle">
              <div class="flex flex-wrap items-center gap-1">
                <span v-if="m.type === 'video' && (m.durations || []).length >= 2" class="cfg-tag">{{ m.durations[0] }}–{{ m.durations[m.durations.length - 1] }}</span>
                <span v-else-if="m.type === 'video' && (m.durations || []).length === 1" class="cfg-tag">{{ m.durations[0] }}</span>
                <span v-if="m.type === 'image'" v-for="r in (m.resolutions || [])" :key="'ir'+r" class="cfg-tag">{{ r }}</span>
                <span v-if="m.reference_mode === 'frame' && m.max_reference_images > 0" class="cfg-tag">首尾帧{{ Math.min(m.max_reference_images, 2) }}</span>
                <span v-if="m.reference_mode === 'frame' && m.max_reference_images > 2" class="cfg-tag">参考图{{ refImages(m) }}</span>
                <span v-else-if="m.reference_mode && m.reference_mode !== 'none' && m.reference_mode !== 'frame' && m.max_reference_images > 0" class="cfg-tag">参考图{{ refImages(m) }}</span>
                <span v-else-if="m.type === 'image' && m.image_to_image" class="cfg-tag">参考图</span>
                <template v-if="caps(m)">
                  <span v-if="caps(m).videos" class="cfg-tag">视频{{ caps(m).videos }}</span>
                  <span v-if="caps(m).audios" class="cfg-tag">音频{{ caps(m).audios }}</span>
                </template>
                <span v-for="r in (m.ratios || [])" :key="'rt'+r" class="cfg-tag">{{ r }}</span>
                <span v-if="capEmpty(m)" class="text-white/30 text-xs">—</span>
              </div>
            </td>

            <!-- Successful generations to date -->
            <td class="px-3 py-3.5 align-middle text-right tabular-nums whitespace-nowrap"
                :class="m.generation_count > 0 ? 'text-white/85 font-medium' : 'text-white/25'">
              {{ (m.generation_count || 0).toLocaleString('en-US') }}
            </td>

            <!-- 配价即上架（无开关）：全档位有价 + 绑定了启用渠道才显示「已上架」 -->
            <td class="px-3 py-3.5 align-middle">
              <span class="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-medium ring-1"
                    :class="m.sellable
                      ? 'bg-emerald-500/10 text-emerald-600 ring-emerald-500/25'
                      : 'bg-white/[0.05] text-white/40 ring-white/10'"
                    :title="m.sellable ? '全部档位有价且有启用渠道,对前台可见' : '配齐全部档位价格并绑定启用渠道后自动上架'">
                {{ m.sellable ? '已上架' : '未上架' }}
              </span>
            </td>

            <!-- Actions — small soft buttons, danger variant for delete -->
            <td class="px-3 py-3.5 align-middle text-right whitespace-nowrap">
              <div class="inline-flex items-center gap-1">
                <button @click="testing = m" class="act" title="测试生成">
                  <Icon name="test" class="w-3.5 h-3.5" />
                </button>
                <button @click="openEdit(m)" class="act" title="编辑">
                  <Icon name="config" class="w-3.5 h-3.5" />
                </button>
                <button @click="remove(m)" class="act danger" title="删除">
                  <Icon name="trash" class="w-3.5 h-3.5" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <ModelFormModal v-if="showForm" :model="editing" @close="showForm = false" @saved="onSaved" />
    <TestModal v-if="testing" :model="testing" @close="testing = null" />
  </section>
</template>

<style scoped>
/* --- type tag (vivid 同款：图像蓝底 / 视频墨底) */
.kind-tag {
  display: inline-flex; padding: 3px 10px; border-radius: 999px;
  font-size: 12px; font-weight: 700; white-space: nowrap;
}
.kind-image { background: rgb(14 165 233 / 0.12); color: rgb(2 132 199); }
.kind-video { background: rgb(24 24 27 / 0.1); color: rgb(24 24 27); }

/* --- pricing chip: 档位 + 普通价(绿) + 代理价(琥珀) --- */
.p-chip {
  display: inline-flex; align-items: baseline; gap: 6px;
  padding: 3px 9px; border-radius: 8px;
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  font-size: 12px; white-space: nowrap;
}
.p-chip b { color: var(--fg); font-weight: 600; }
.p-chip em { font-style: normal; font-weight: 700; color: rgb(5 150 105); font-variant-numeric: tabular-nums; }
.p-chip em.ag { color: rgb(180 83 9); font-weight: 600; }
.p-chip em.ag::before { content: '代理 '; font-size: 10px; font-weight: 500; opacity: 0.75; }

/* --- config tags: uniform neutral mono chips (时长 / 分辨率 / 参考 / 比例) --- */
.cfg-tag {
  display: inline-flex; align-items: center;
  padding: 2px 8px; border-radius: 6px;
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  font-size: 11px; font-weight: 600; color: var(--fg-2);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  white-space: nowrap;
}

/* --- compact square icon button for the actions column --- */
.act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.9rem;
  height: 1.9rem;
  border-radius: 0.5rem;
  color: var(--fg-2);
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  transition: background 0.15s, color 0.15s, box-shadow 0.15s;
}
.act:hover { background: var(--hover); color: var(--fg); }
.act.danger {
  color: rgb(225 29 72);
  background: rgb(244 63 94 / 0.08);
  box-shadow: inset 0 0 0 1px rgb(244 63 94 / 0.22);
}
.act.danger:hover {
  color: #fff;
  background: rgb(225 29 72);
}
</style>
