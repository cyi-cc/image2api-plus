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
const okMsg = ref('')         // 同步模型成功提示

// create/edit form
const form = ref(null)
const saving = ref(false)
const formErr = ref('')

// 同步上游模型弹窗：开关选择 + 别名，批量导入（保存渠道已自动全量同步）
const fetchTarget = ref(null)   // saved channel being synced
const fetching = ref(false)
const fetchErr = ref('')
const specs = ref([])
const picked = ref(new Set())
const importing = ref(false)

async function load() {
  loading.value = true
  err.value = ''
  const r = await api('/channels')
  if (r.ok) channels.value = r.data?.data || []
  else err.value = r.data?.detail || '渠道加载失败'
  loading.value = false
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
  okMsg.value = ''
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

// 行内「同步上游模型」走开关 + 别名弹窗（渠道已保存，用库里凭据拉取）。
// 默认全开：点「同步模型」把勾选项批量导入绑定；上游消失的模型由保存时的
// 全量同步负责解绑。
async function openFetch(c) {
  fetchTarget.value = c
  specs.value = []
  picked.value = new Set()
  fetchErr.value = ''
  fetching.value = true
  const r = await api('/channels/fetch-models', jsonBody('POST', { id: c.id }))
  fetching.value = false
  if (!r.ok) { fetchErr.value = r.data?.detail || '拉取失败'; return }
  specs.value = (r.data?.data || []).map((s) => ({ ...s, alias: '' }))
  picked.value = new Set(specs.value.map((s) => s.id))
}

function togglePick(id) {
  const next = new Set(picked.value)
  next.has(id) ? next.delete(id) : next.add(id)
  picked.value = next
}

async function doImport() {
  if (importing.value || !picked.value.size) return
  const dup = specs.value.find((s) => picked.value.has(s.id) && s.alias && s.alias === s.id)
  if (dup) { fetchErr.value = `${dup.id}的别名与上游模型名相同，别名只在需要改名时设置，同名请留空`; return }
  const models = specs.value.filter((s) => picked.value.has(s.id))
    .map((s) => { const { bound, ...rest } = s; return rest })
  importing.value = true
  fetchErr.value = ''
  try {
    const r = await api('/channels/import-models', jsonBody('POST', { id: fetchTarget.value.id, models }))
    if (!r.ok) { fetchErr.value = r.data?.detail || '导入失败'; return }
    okMsg.value = `已导入 ${models.length} 个模型`
    fetchTarget.value = null
    load()
  } finally {
    importing.value = false
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
    <div class="page-head">
      <div>
        <h2 class="page-title">渠道管理</h2>
        <p class="page-sub">同构 /v1 协议上游实例 · 保存时自动同步上游模型 · 余额实时探测</p>
      </div>
      <div class="page-actions">
        <button @click="load" class="refresh-btn" title="刷新" aria-label="刷新"><Icon name="refresh" :size="18" /></button>
      </div>
    </div>

    <div class="page-bar"><button @click="openAdd" class="btn-primary"><Icon name="plus" class="w-4 h-4" /> 添加渠道</button></div>

    <p v-if="err" class="text-xs text-rose-600">{{ err }}</p>
    <p v-if="warn" class="text-xs text-amber-600">{{ warn }}</p>
    <p v-if="okMsg" class="text-xs text-emerald-600">{{ okMsg }}</p>

    <div class="card overflow-hidden">
      <div class="overflow-x-auto">
        <table class="tbl">
          <thead>
            <tr>
              <th>渠道</th>
              <th>并发</th>
              <th>配比</th>
              <th class="text-right">运行中</th>
              <th class="text-right">成功数</th>
              <th class="text-right">失败数</th>
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
              <td class="tabular-nums">{{ c.concurrency || '不限' }}</td>
              <td class="tabular-nums">{{ c.weight || 1 }}</td>
              <td class="text-right tabular-nums" :class="c.running_count > 0 ? 'text-emerald-600' : ''">{{ c.running_count || 0 }}</td>
              <td class="text-right tabular-nums">{{ (c.success_total || 0).toLocaleString() }}</td>
              <td class="text-right tabular-nums" :class="c.fail_count > 0 ? 'text-rose-600' : ''">{{ (c.fail_count || 0).toLocaleString() }}</td>
              <td class="text-right tabular-nums">{{ (c.used_credits || 0).toLocaleString() }}</td>
              <td class="text-right tabular-nums">{{ c.upstream_balance == null ? '-' : c.upstream_balance.toLocaleString() }}</td>
              <td>
                <button @click="toggle(c)" class="inline-flex items-center gap-1.5 text-xs"
                        :class="c.enabled ? 'text-emerald-600' : 'text-white/40'">
                  <span class="w-1.5 h-1.5 rounded-full" :class="c.enabled ? 'bg-emerald-500' : 'bg-white/25'"></span>
                  {{ c.enabled ? '启用' : '停用' }}
                </button>
              </td>
              <td class="text-xs text-white/45 whitespace-nowrap">{{ c.last_used_at ? fmtTs(c.last_used_at) : '-' }}</td>
              <td class="text-right whitespace-nowrap">
                <button @click="openFetch(c)" class="op" title="同步上游模型"><Icon name="spark" class="w-3.5 h-3.5" /></button>
                <button @click="openEdit(c)" class="op" title="编辑"><Icon name="config" class="w-3.5 h-3.5" /></button>
                <button @click="del(c)" class="op danger" title="删除"><Icon name="trash" class="w-3.5 h-3.5" /></button>
              </td>
            </tr>
            <tr v-if="loading"><td colspan="11"><div class="empty"><span class="empty-text">正在加载渠道…</span></div></td></tr>
            <tr v-else-if="!channels.length"><td colspan="11"><div class="empty">
              <span class="empty-ico"><Icon name="plug" class="w-6 h-6" /></span>
              <span class="empty-text">还没有渠道</span>
              <span class="empty-sub">点上方「添加渠道」接入上游实例</span>
            </div></td></tr>
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
            <label class="lbl">上游地址 <span class="text-rose-600">*</span></label>
            <input v-model.trim="form.base_url" type="text" maxlength="300" class="field" placeholder="https://example.com" />
            <p class="mt-1 text-[11px] text-white/35">同构 /v1 协议实例的站点地址，不带路径</p>
          </div>
          <div>
            <label class="lbl">API Key <span v-if="!form.id" class="text-rose-600">*</span></label>
            <input v-model.trim="form.api_key" type="text" class="field" :placeholder="form.id ? '留空保持不变' : 'vk_…'" autocomplete="off" />
            <p class="mt-1 text-[11px] text-white/35">上游站点的用户 API Key（vk_ 前缀），密文存储，不回显</p>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="lbl">并发数</label>
              <input v-model.number="form.concurrency" type="number" min="0" max="2147483647" class="field" />
              <p class="mt-1 text-[11px] text-white/35">0 = 不限</p>
            </div>
            <div>
              <label class="lbl">配比</label>
              <input v-model.number="form.weight" type="number" min="1" max="100" class="field" />
              <p class="mt-1 text-[11px] text-white/35">同模型多渠道按配比分单</p>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <label class="lbl !mb-0">状态</label>
            <button type="button" @click="form.enabled = !form.enabled" class="sw shrink-0" :class="{ 'sw-on': form.enabled }" role="switch" :aria-checked="form.enabled">
              <span class="sw-thumb"></span>
            </button>
            <span class="text-xs" :class="form.enabled ? 'text-emerald-600' : 'text-[var(--fg-3)]'">{{ form.enabled ? '已启用' : '已停用' }}</span>
          </div>
          <p v-if="formErr" class="text-xs text-rose-600">{{ formErr }}</p>
        </div>
        <div class="px-5 py-4 border-t border-white/[0.06] flex justify-end gap-2">
          <button @click="form = null" class="btn-ghost">取消</button>
          <button @click="save" :disabled="saving" class="btn-primary">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- 同步上游模型：开关选择 + 别名，批量导入 -->
    <div v-if="fetchTarget" class="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-start justify-center overflow-y-auto p-4" @click.self="fetchTarget = null">
      <div class="card !shadow-2xl my-12 w-full max-w-2xl">
        <div class="px-5 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 class="text-sm font-semibold">同步上游模型 · {{ fetchTarget.name || fetchTarget.base_url }}</h2>
          <button @click="fetchTarget = null" class="text-white/40 hover:text-white transition-colors"><Icon name="close" class="w-5 h-5" /></button>
        </div>
        <div class="p-5">
          <div v-if="fetching" class="empty !py-10"><span class="empty-text">正在从上游拉取模型列表…</span></div>
          <template v-else>
            <p class="text-xs text-white/45 mb-3 leading-relaxed">开启的模型会同步到本站模型列表，参数随上游带入；到「模型管理」配价后上架。别名即本站模型标识，留空 = 与上游同名。</p>
            <p v-if="fetchErr" class="text-xs text-rose-600 mb-3">{{ fetchErr }}</p>
            <div v-if="!specs.length" class="empty !py-10"><span class="empty-text">上游没有可售模型（模型需在上游已配价）</span></div>
            <div v-else class="space-y-2 max-h-[400px] overflow-y-auto p-0.5">
              <div v-for="s in specs" :key="s.id" @click="togglePick(s.id)"
                   class="p-3 rounded-xl ring-1 ring-white/10 bg-white/[0.02] hover:bg-white/[0.04] hover:ring-white/20 cursor-pointer transition-all space-y-2"
                   :class="!picked.has(s.id) && 'opacity-45'">
                <div class="flex items-center gap-2.5 min-w-0">
                  <span class="w-2 h-2 rounded-full shrink-0"
                        :class="s.kind === 'video' ? 'bg-sky-500' : s.kind === 'text' ? 'bg-amber-500' : 'bg-zinc-500'"></span>
                  <code class="font-mono text-[13px] font-semibold text-white/90 truncate">{{ s.id }}</code>
                  <span class="text-[11px] font-semibold text-white/40 shrink-0">{{ KIND_LABEL[s.kind] || s.kind }}</span>
                  <button type="button" class="ml-auto sw" :class="picked.has(s.id) && 'sw-on'"
                          :aria-pressed="picked.has(s.id)" @click.stop="togglePick(s.id)">
                    <span class="sw-thumb"></span>
                  </button>
                </div>
                <div class="pl-[18px] text-[11px] text-white/50">{{ specText(s) }}</div>
                <div class="pl-[18px]" @click.stop>
                  <input v-model.trim="s.alias" type="text" maxlength="64" :disabled="!picked.has(s.id)"
                         class="w-full px-2.5 py-1.5 rounded-lg bg-white/[0.03] border border-dashed border-white/15 text-xs text-white/80
                                hover:border-solid focus:border-solid focus:border-white/30 focus:bg-white/[0.05] focus:outline-none
                                disabled:opacity-40 disabled:cursor-not-allowed transition-all"
                         placeholder="别名（留空 = 与上游同名）" />
                </div>
              </div>
            </div>
          </template>
        </div>
        <div class="px-5 py-4 border-t border-white/[0.06] flex justify-end gap-2">
          <button @click="fetchTarget = null" class="btn-ghost">关闭</button>
          <button @click="doImport" :disabled="importing || !picked.size" class="btn-primary">
            {{ importing ? '同步中…' : `同步模型（${picked.size}）` }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.tbl { width: 100%; border-collapse: collapse; min-width: 1000px; }
.tbl thead th { padding: 10px 14px; text-align: left; font-size: 11px; font-weight: 600; color: var(--fg-3); border-bottom: 1px solid var(--hairline); white-space: nowrap; }
.tbl tbody td { padding: 10px 14px; border-bottom: 1px solid var(--hairline); font-size: 12.5px; color: var(--fg-2); vertical-align: middle; }
.tbl tbody tr:last-child td { border-bottom: none; }
.tbl tbody tr:hover { background: var(--surface-2); }

.lbl { display: block; font-size: 0.72rem; font-weight: 500; color: var(--fg-3); margin-bottom: 0.4rem; }

.op { display: inline-grid; place-items: center; min-width: 28px; height: 26px; padding: 0 7px; margin-left: 4px; border-radius: 7px; background: var(--surface-2); color: var(--fg-2); }
.op:hover { background: var(--hover); color: var(--fg); }
.op.danger:hover { background: rgb(244 63 94 / 0.15); color: rgb(251 113 133); }

/* iOS-style switch for 模型开关 in the sync modal */
.sw { position: relative; width: 2rem; height: 1.15rem; border-radius: 9999px; background: var(--hover); box-shadow: inset 0 0 0 1px var(--hairline); transition: background 0.18s ease; flex-shrink: 0; }
.sw-thumb { position: absolute; top: 2px; left: 2px; width: calc(1.15rem - 4px); height: calc(1.15rem - 4px); border-radius: 9999px; background: var(--surface-solid); box-shadow: 0 1px 2px rgb(15 23 42 / 0.3); transition: transform 0.18s ease; }
.sw-on { background: #18181b; box-shadow: inset 0 0 0 1px #18181b; }
.sw-on .sw-thumb { transform: translateX(calc(2rem - 1.15rem)); }
</style>
