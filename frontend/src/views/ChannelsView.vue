<script setup>
import { ref, onMounted } from 'vue'
import { api, jsonBody } from '../api'
import { fmtTs, fmtDate } from '../utils/format'
import Icon from '../components/Icon.vue'

const KIND_LABEL = { image: '图像', video: '视频', text: '文本' }

const channels = ref([])
const loading = ref(false)
const err = ref('')
const warn = ref('')          // 保存后自动同步的非致命告警

// create/edit form
const form = ref(null)
const saving = ref(false)
const formErr = ref('')

// fetch-models → 勾选即绑定（选中自动导入到模型列表，取消即解绑）
const fetchTarget = ref(null)   // channel snapshot being imported into
const fetching = ref(false)
const fetchErr = ref('')
const specs = ref([])

async function load() {
  loading.value = true
  err.value = ''
  const r = await api('/channels')
  if (r.ok) channels.value = r.data?.data || []
  else err.value = r.data?.detail || '渠道加载失败'
  loading.value = false
}

function modelSummary(c) {
  const names = c.models || []
  if (!names.length) return ''
  return names.length <= 2 ? names.join('、') : `${names[0]}、${names[1]} 等`
}

function openAdd() {
  formErr.value = ''
  form.value = { id: '', name: '', base_url: '', api_key: '', concurrency: 5, weight: 1, enabled: true }
}
function openEdit(c) {
  formErr.value = ''
  form.value = { id: c.id, name: c.name, base_url: c.base_url, api_key: '', concurrency: c.concurrency || 0, weight: c.weight || 1, enabled: !!c.enabled }
}

async function save() {
  const f = form.value
  if (!f) return
  if (!/^https?:\/\/\S+$/.test((f.base_url || '').trim())) { formErr.value = '上游地址需以 http(s):// 开头'; return }
  if (!f.id && !f.api_key) { formErr.value = '请填写 API Key'; return }
  if (saving.value) return
  saving.value = true
  formErr.value = ''
  const r = await api('/channels', jsonBody('POST', {
    id: f.id, name: f.name, base_url: f.base_url, api_key: f.api_key,
    enabled: !!f.enabled, concurrency: f.concurrency || 0, weight: f.weight || 1,
  }))
  saving.value = false
  if (!r.ok) { formErr.value = r.data?.detail || '保存失败'; return }
  warn.value = r.data?.warning || ''
  form.value = null
  load()
}

async function toggle(c) {
  const prev = !!c.enabled
  c.enabled = !prev
  const r = await api('/channels', jsonBody('POST', {
    id: c.id, name: c.name, base_url: c.base_url, api_key: '',
    enabled: c.enabled, concurrency: c.concurrency || 0, weight: c.weight || 1,
  }))
  if (!r.ok) { c.enabled = prev; err.value = r.data?.detail || '状态更新失败'; return }
  warn.value = r.data?.warning || ''
}

async function del(c) {
  if (!confirm(`确认删除渠道「${c.name || c.base_url}」？其绑定的模型若没有其他渠道接入，将一并删除。`)) return
  const r = await api('/channels/delete', jsonBody('POST', { id: c.id }))
  if (!r.ok) { err.value = r.data?.detail || '删除失败'; return }
  channels.value = channels.value.filter((x) => x.id !== c.id)
}

// 表单内拉模型：编辑态走库内渠道（密钥不用重填）；新渠道用表单里的地址+key 预览。
function fetchFromForm() {
  const f = form.value
  if (!f) return
  if (!/^https?:\/\/\S+$/.test((f.base_url || '').trim())) { formErr.value = '上游地址需以 http(s):// 开头'; return }
  if (!f.id && !f.api_key) { formErr.value = '请填写 API Key'; return }
  openFetch({ id: f.id || '', name: f.name || f.base_url, base_url: f.base_url, api_key: f.api_key, concurrency: f.concurrency, weight: f.weight, enabled: !!f.enabled })
}

async function openFetch(c) {
  fetchTarget.value = c
  specs.value = []
  fetchErr.value = ''
  fetching.value = true
  const r = await api('/channels/fetch-models', jsonBody('POST', {
    id: c.id || '', base_url: c.base_url || '', api_key: c.api_key || '',
  }))
  fetching.value = false
  if (!r.ok) { fetchErr.value = r.data?.detail || '拉取失败'; return }
  specs.value = (r.data?.data || []).map((s) => ({ ...s, alias: s.alias || '', busy: false }))
}

// 勾选 = 立即导入绑定；取消勾选 = 解绑（模型没有其他渠道接入时本地行一并删除）。
// 新渠道尚未入库时，第一次勾选会先自动保存渠道再绑定。
async function toggleBind(s) {
  if (s.busy) return
  s.busy = true
  fetchErr.value = ''
  try {
    const target = fetchTarget.value
    if (!s.bound) {
      if (!target.id) {
        const sr = await api('/channels', jsonBody('POST', {
          id: '', name: target.name || target.base_url, base_url: target.base_url, api_key: target.api_key,
          enabled: target.enabled !== false, concurrency: target.concurrency || 0, weight: target.weight || 1,
        }))
        if (!sr.ok) { fetchErr.value = sr.data?.detail || '渠道保存失败'; return }
        if (sr.data?.warning) warn.value = sr.data.warning
        target.id = sr.data?.id
        form.value = null
      }
      const { bound, busy, ...rest } = s
      const r = await api('/channels/import-models', jsonBody('POST', { id: target.id, models: [rest] }))
      if (!r.ok) { fetchErr.value = r.data?.detail || '导入失败'; return }
      s.bound = true
    } else {
      const r = await api('/channels/unbind-model', jsonBody('POST', { id: target.id, model: s.id }))
      if (!r.ok) { fetchErr.value = r.data?.detail || '解绑失败'; return }
      s.bound = false
    }
    load()
  } finally {
    s.busy = false
  }
}

function specText(s) {
  const parts = []
  if (s.qualities?.length) parts.push(s.qualities.join('/'))
  if (s.duration) {
    if (s.duration.mode === 'fixed') parts.push(`固定 ${s.duration.seconds}s`)
    else if (s.duration.mode === 'range') parts.push(`${s.duration.min}-${s.duration.max}s`)
    else if (s.duration.mode === 'options') parts.push((s.duration.options || []).map((d) => `${d}s`).join('/'))
  }
  const refs = []
  if (s.refImages > 0) refs.push(`图片${s.refImages}`)
  if (s.refVideos > 0) refs.push(`视频${s.refVideos}${s.refVideoSeconds > 0 ? `≤${s.refVideoSeconds}s` : ''}`)
  if (s.refAudios > 0) refs.push(`音频${s.refAudios}${s.refAudioSeconds > 0 ? `≤${s.refAudioSeconds}s` : ''}`)
  if (refs.length) parts.push(`参考 ${refs.join('+')}`)
  return parts.join(' · ') || '-'
}

onMounted(load)
</script>

<template>
  <section class="space-y-4">
    <div class="card p-4 flex items-center justify-between gap-3 flex-wrap">
      <div>
        <div class="text-sm font-medium">渠道</div>
        <p class="text-xs text-white/45 mt-0.5">同构 /v1 协议上游实例 · 保存时自动同步上游模型 · 余额实时探测</p>
      </div>
      <button @click="openAdd" class="btn-primary">
        <Icon name="plus" class="w-4 h-4" /> 添加渠道
      </button>
    </div>

    <p v-if="err" class="text-xs text-rose-300">{{ err }}</p>
    <p v-if="warn" class="text-xs text-amber-300">{{ warn }}</p>

    <div class="card overflow-hidden">
      <div class="overflow-x-auto">
        <table class="tbl">
          <thead>
            <tr>
              <th>渠道</th>
              <th>上游地址</th>
              <th>模型</th>
              <th>并发</th>
              <th>配比</th>
              <th class="text-right">成功调用</th>
              <th class="text-right">累计消耗</th>
              <th class="text-right">上游余额</th>
              <th>状态</th>
              <th>最近使用</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in channels" :key="c.id">
              <td>
                <div class="font-medium text-white/85">{{ c.name }}</div>
                <div class="text-[11px] text-white/35 font-mono">{{ c.id }}</div>
              </td>
              <td>
                <div class="max-w-[240px] truncate font-mono text-xs text-white/55" :title="c.base_url">{{ c.base_url }}</div>
              </td>
              <td>
                <div class="tabular-nums text-white/80">{{ (c.models || []).length }}</div>
                <div class="text-[11px] text-white/35 max-w-[180px] truncate" :title="(c.models || []).join('、')">{{ modelSummary(c) }}</div>
              </td>
              <td class="tabular-nums">{{ c.concurrency || '默认' }}</td>
              <td class="tabular-nums">{{ c.weight || 1 }}</td>
              <td class="text-right tabular-nums">{{ (c.success_total || 0).toLocaleString() }}</td>
              <td class="text-right tabular-nums">{{ (c.used_credits || 0).toLocaleString() }}</td>
              <td class="text-right tabular-nums">{{ c.upstream_balance == null ? '-' : c.upstream_balance.toLocaleString() }}</td>
              <td>
                <button @click="toggle(c)" class="inline-flex items-center gap-1.5 text-xs"
                        :class="c.enabled ? 'text-emerald-300' : 'text-white/40'">
                  <span class="w-1.5 h-1.5 rounded-full" :class="c.enabled ? 'bg-emerald-400' : 'bg-white/25'"></span>
                  {{ c.enabled ? '启用' : '停用' }}
                </button>
              </td>
              <td class="text-xs text-white/45 whitespace-nowrap">{{ c.last_used_at ? fmtTs(c.last_used_at) : '-' }}</td>
              <td class="text-right whitespace-nowrap">
                <button @click="openFetch(c)" class="op" title="拉取上游模型"><Icon name="spark" class="w-3.5 h-3.5" /></button>
                <button @click="openEdit(c)" class="op" title="编辑"><Icon name="config" class="w-3.5 h-3.5" /></button>
                <button @click="del(c)" class="op danger" title="删除"><Icon name="trash" class="w-3.5 h-3.5" /></button>
              </td>
            </tr>
            <tr v-if="loading"><td colspan="11" class="py-10 text-center text-white/40 text-sm">正在加载渠道…</td></tr>
            <tr v-else-if="!channels.length"><td colspan="11" class="py-10 text-center text-white/40 text-sm">没有渠道，点右上角「添加渠道」接入上游实例</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 添加 / 编辑渠道 -->
    <div v-if="form" class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-start justify-center overflow-y-auto p-4" @click.self="form = null">
      <div class="card !shadow-2xl my-12 w-full max-w-lg">
        <div class="px-5 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 class="text-sm font-semibold">{{ form.id ? '编辑渠道' : '添加渠道' }}</h2>
          <button @click="form = null" class="text-white/40 hover:text-white transition-colors"><Icon name="close" class="w-5 h-5" /></button>
        </div>
        <div class="p-5 space-y-4">
          <div>
            <label class="lbl">渠道名称</label>
            <input v-model.trim="form.name" type="text" maxlength="64" class="field" placeholder="例如：主站" />
          </div>
          <div>
            <label class="lbl">上游地址 <span class="text-rose-300">*</span></label>
            <input v-model.trim="form.base_url" type="text" maxlength="300" class="field" placeholder="https://example.com" />
            <p class="mt-1 text-[11px] text-white/35">同构 /v1 协议实例的站点地址，不带路径</p>
          </div>
          <div>
            <label class="lbl">API Key <span v-if="!form.id" class="text-rose-300">*</span></label>
            <input v-model.trim="form.api_key" type="text" class="field" :placeholder="form.id ? '留空保持不变' : 'vk_…'" autocomplete="off" />
            <p class="mt-1 text-[11px] text-white/35">上游站点的用户 API Key（vk_ 前缀），密文存储，不回显</p>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="lbl">并发数</label>
              <input v-model.number="form.concurrency" type="number" min="0" max="100" class="field" />
              <p class="mt-1 text-[11px] text-white/35">0 = 默认 5</p>
            </div>
            <div>
              <label class="lbl">配比</label>
              <input v-model.number="form.weight" type="number" min="1" max="100" class="field" />
              <p class="mt-1 text-[11px] text-white/35">同模型多渠道按配比分单</p>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <label class="lbl !mb-0">状态</label>
            <button type="button" @click="form.enabled = !form.enabled" class="inline-flex items-center gap-1.5 text-xs"
                    :class="form.enabled ? 'text-emerald-300' : 'text-white/40'">
              <span class="w-1.5 h-1.5 rounded-full" :class="form.enabled ? 'bg-emerald-400' : 'bg-white/25'"></span>
              {{ form.enabled ? '已启用' : '已停用' }}
            </button>
          </div>
          <div class="flex items-center gap-3">
            <button type="button" @click="fetchFromForm" :disabled="fetching" class="btn-ghost text-xs">
              {{ fetching ? '拉取中…' : '获取模型列表' }}
            </button>
            <span class="text-[11px] text-white/35">{{ form.id ? '用已保存的地址与密钥拉取' : '用上方的地址与 API Key 拉取；导入时会先自动保存渠道' }}</span>
          </div>
          <p v-if="formErr" class="text-xs text-rose-300">{{ formErr }}</p>
        </div>
        <div class="px-5 py-4 border-t border-white/[0.06] flex justify-end gap-2">
          <button @click="form = null" class="btn-ghost">取消</button>
          <button @click="save" :disabled="saving" class="btn-primary">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- 拉取上游模型 → 勾选即导入绑定 -->
    <div v-if="fetchTarget" class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-start justify-center overflow-y-auto p-4" @click.self="fetchTarget = null">
      <div class="card !shadow-2xl my-12 w-full max-w-2xl">
        <div class="px-5 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 class="text-sm font-semibold">上游模型 · {{ fetchTarget.name || fetchTarget.base_url }}</h2>
          <button @click="fetchTarget = null" class="text-white/40 hover:text-white transition-colors"><Icon name="close" class="w-5 h-5" /></button>
        </div>
        <div class="p-5">
          <div v-if="fetching" class="py-10 text-center text-white/40 text-sm">正在从上游拉取模型列表…</div>
          <template v-else>
            <p class="text-xs text-white/45 mb-3">勾选即导入并绑定到本渠道，取消勾选即解绑（模型没有其他渠道接入时本地行一并删除）。能力参数随模型带入，导入后到「模型管理」配价上架。别名即本站模型标识（留空=与上游同名，绑定时生效）。注意：保存渠道会按上游清单全量重同步，手动解绑的模型若仍在上游列表中会被重新接入。</p>
            <p v-if="fetchErr" class="text-xs text-rose-300 mb-3">{{ fetchErr }}</p>
            <div v-if="!specs.length" class="py-10 text-center text-white/40 text-sm">上游没有可售模型（模型需在上游已配价）</div>
            <div v-else class="space-y-1.5 max-h-[380px] overflow-y-auto">
              <div v-for="s in specs" :key="s.id" @click="toggleBind(s)"
                   class="flex items-start gap-2.5 p-2.5 rounded-lg ring-1 ring-white/10 hover:bg-white/[0.04] cursor-pointer transition-colors"
                   :class="[!s.bound && 'opacity-50', s.busy && 'pointer-events-none']">
                <span class="mt-1 inline-block h-3.5 w-3.5 shrink-0 rounded-full ring-1 transition-all"
                      :class="s.bound ? 'bg-sky-400 ring-sky-300' : 'bg-transparent ring-white/25'"></span>
                <div class="flex-1 min-w-0 space-y-1.5">
                  <div class="flex items-center gap-2 flex-wrap">
                    <code class="font-mono text-xs text-white/85">{{ s.id }}</code>
                    <span class="px-1.5 py-0.5 rounded text-[10px] font-semibold"
                          :class="s.kind === 'video' ? 'bg-rose-400/15 text-rose-300' : 'bg-sky-400/15 text-sky-300'">
                      {{ KIND_LABEL[s.kind] || s.kind }}
                    </span>
                    <span class="text-[11px] text-white/40">{{ specText(s) }}</span>
                    <span v-if="s.busy" class="ml-auto text-[10px] text-white/40">处理中…</span>
                    <span v-else-if="s.bound" class="ml-auto px-1.5 py-0.5 rounded bg-emerald-400/15 text-emerald-300 text-[10px] font-semibold">已接入</span>
                  </div>
                  <div class="flex items-center gap-2" @click.stop>
                    <span class="text-[11px] text-white/35 shrink-0">别名</span>
                    <input v-model.trim="s.alias" type="text" maxlength="64" :disabled="s.bound"
                           class="flex-1 min-w-0 px-2.5 py-1 rounded-lg bg-white/[0.04] ring-1 ring-white/10 text-xs text-white/80 disabled:opacity-40"
                           :placeholder="s.bound ? '已绑定，到模型管理改别名' : '留空 = 与上游同名'" />
                  </div>
                </div>
              </div>
            </div>
          </template>
        </div>
        <div class="px-5 py-4 border-t border-white/[0.06] flex justify-end gap-2">
          <button @click="fetchTarget = null" class="btn-ghost">关闭</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.tbl { width: 100%; border-collapse: collapse; min-width: 1000px; }
.tbl thead th { padding: 10px 14px; text-align: left; font-size: 11px; font-weight: 600; color: rgb(255 255 255 / 0.45); border-bottom: 1px solid rgb(255 255 255 / 0.06); white-space: nowrap; }
.tbl tbody td { padding: 10px 14px; border-bottom: 1px solid rgb(255 255 255 / 0.04); font-size: 12.5px; color: rgb(255 255 255 / 0.7); vertical-align: middle; }
.tbl tbody tr:last-child td { border-bottom: none; }
.tbl tbody tr:hover { background: rgb(255 255 255 / 0.02); }

.lbl { display: block; font-size: 0.72rem; font-weight: 500; color: rgb(255 255 255 / 0.55); margin-bottom: 0.4rem; }

.op { display: inline-grid; place-items: center; min-width: 28px; height: 26px; padding: 0 7px; margin-left: 4px; border-radius: 7px; background: rgb(255 255 255 / 0.05); color: rgb(255 255 255 / 0.6); }
.op:hover { background: rgb(255 255 255 / 0.1); color: white; }
.op.danger:hover { background: rgb(244 63 94 / 0.15); color: rgb(251 113 133); }
</style>
