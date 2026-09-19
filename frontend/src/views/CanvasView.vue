<script setup>
// 无限画布页面:文档管理(多画布/自动保存)、生成面板、撤销重做、快捷键。
// 交互细节全部在 CanvasStage;这里只做数据编排。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { api, generatedUrl, jsonBody, thumbUrl } from '../api'
import { auth } from '../auth'
import { normalizeDoc, newId, ratioToSize, serializeDoc, screenToWorld } from '../utils/canvas'
import CanvasStage from '../components/canvas/CanvasStage.vue'

const stage = ref(null)

// ---- 文档状态 ----
const canvases = ref([])
const currentId = ref('')
const title = ref('未命名画布')
const doc = reactive({ viewport: { x: 0, y: 0, scale: 1 }, nodes: [], edges: [] })
const loading = ref(true)
const loadError = ref('')
const saveState = ref('saved') // saved | dirty | saving | error
const scalePct = ref(100)

// ---- 选择 / 预览 ----
const selectionIds = ref([])
const previewSrc = ref('')

// ---- 生成面板 ----
const allModels = ref([])
const gen = reactive({ modelId: '', prompt: '', ratio: '' })
const genBusy = ref(false)
const genMsg = ref('')
const imageModels = computed(() =>
  (allModels.value || []).filter((m) => m.enabled !== false && m.type === 'image'),
)
const model = computed(() => imageModels.value.find((m) => m.id === gen.modelId) || null)
const ratios = computed(() => model.value?.ratios?.length ? model.value.ratios : ['1:1'])
const selectedImages = computed(() =>
  selectionIds.value
    .map((id) => doc.nodes.find((n) => n.id === id))
    .filter((n) => n && n.type === 'image'),
)
const selectedNotes = computed(() =>
  selectionIds.value
    .map((id) => doc.nodes.find((n) => n.id === id))
    .filter((n) => n && n.type === 'note' && (n.text || '').trim()),
)

// ---- 我的图片插入 ----
const pickerOpen = ref(false)
const pickerItems = ref([])
const pickerLoading = ref(false)

// ================= 文档加载 / 保存 =================

async function loadList() {
  const r = await api('/canvas')
  if (!r.ok) throw new Error(r.data?.detail || '加载画布列表失败')
  canvases.value = r.data?.data || []
}

async function loadCanvas(id) {
  const r = await api(`/canvas/${id}`)
  if (!r.ok) throw new Error(r.data?.detail || '加载画布失败')
  const d = normalizeDoc(safeParse(r.data?.data))
  // 页面刷新会丢掉进行中的生成任务结果(无轮询),如实标记中断。
  for (const n of d.nodes) {
    if (n.type === 'gen' && n.status === 'running') {
      n.status = 'failed'
      n.error = '页面已刷新,任务中断'
    }
  }
  currentId.value = r.data.id
  title.value = r.data.title || '未命名画布'
  doc.viewport = d.viewport
  doc.nodes = d.nodes
  doc.edges = d.edges
  selectionIds.value = []
  resetHistory()
  saveState.value = 'saved'
  await nextTick()
  stage.value?.setViewport(d.viewport)
  stage.value?.syncZTop()
}

function safeParse(s) {
  try {
    return typeof s === 'string' ? JSON.parse(s) : s
  } catch {
    return null
  }
}

async function boot() {
  loading.value = true
  loadError.value = ''
  try {
    await loadList()
    if (!canvases.value.length) {
      const r = await api('/canvas', jsonBody('POST', { title: '我的画布' }))
      if (!r.ok) throw new Error(r.data?.detail || '创建画布失败')
      await loadList()
    }
    await loadCanvas(canvases.value[0].id)
  } catch (e) {
    loadError.value = String(e.message || e)
  } finally {
    loading.value = false
  }
}

// ---- 自动保存 ----
let saveTimer = null
let retryTimer = null

function markDirty() {
  if (!currentId.value) return
  saveState.value = 'dirty'
  clearTimeout(saveTimer)
  saveTimer = setTimeout(saveNow, 900)
}

async function saveNow() {
  if (!currentId.value || saveState.value === 'saving') return
  clearTimeout(saveTimer)
  saveState.value = 'saving'
  try {
    const r = await api(
      `/canvas/${currentId.value}`,
      jsonBody('PUT', { title: title.value, data: serializeDoc(doc) }),
    )
    if (r.ok) {
      saveState.value = 'saved'
      const row = canvases.value.find((c) => c.id === currentId.value)
      if (row) row.title = title.value
    } else {
      saveState.value = 'error'
      clearTimeout(retryTimer)
      retryTimer = setTimeout(saveNow, 4000)
    }
  } catch {
    saveState.value = 'error'
    clearTimeout(retryTimer)
    retryTimer = setTimeout(saveNow, 4000)
  }
}

function flushSync() {
  // 页面卸载时的最后努力(fetch keepalive 不保证,但值得试)
  if (saveState.value === 'dirty' || saveState.value === 'error') {
    const body = JSON.stringify({ title: title.value, data: serializeDoc(doc) })
    try {
      fetch(`/admin/api/canvas/${currentId.value}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          ...(auth.token ? { Authorization: `Bearer ${auth.token}` } : {}),
        },
        body,
        keepalive: true,
      })
    } catch { /* 尽力而为 */ }
  }
}

// ================= 撤销 / 重做 =================

const undoStack = []
const redoStack = []
const undoCount = ref(0)
const redoCount = ref(0)
const HISTORY_CAP = 60
let currentSnap = ''

function syncCounts() {
  undoCount.value = undoStack.length
  redoCount.value = redoStack.length
}
function snapshotContent() {
  return JSON.stringify({ nodes: doc.nodes, edges: doc.edges })
}
function resetHistory() {
  undoStack.length = 0
  redoStack.length = 0
  currentSnap = snapshotContent()
  syncCounts()
}
function pushHistory() {
  const s = snapshotContent()
  if (s === currentSnap) return
  undoStack.push(currentSnap)
  if (undoStack.length > HISTORY_CAP) undoStack.shift()
  redoStack.length = 0
  currentSnap = s
  syncCounts()
  markDirty()
}
function restore(json) {
  const d = JSON.parse(json)
  doc.nodes = d.nodes || []
  doc.edges = d.edges || []
  const alive = new Set(doc.nodes.map((n) => n.id))
  selectionIds.value = selectionIds.value.filter((id) => alive.has(id))
  stage.value?.syncZTop()
}
function undo() {
  if (!undoStack.length) return
  redoStack.push(currentSnap)
  const prev = undoStack.pop()
  restore(prev)
  currentSnap = snapshotContent()
  syncCounts()
  markDirty()
}
function redo() {
  if (!redoStack.length) return
  undoStack.push(currentSnap)
  const next = redoStack.pop()
  restore(next)
  currentSnap = snapshotContent()
  syncCounts()
  markDirty()
}

// ================= 节点操作 =================

function addNote() {
  const c = viewportCenterWorld()
  const n = {
    id: newId('t'), type: 'note', text: '',
    x: Math.round(c.x - 130), y: Math.round(c.y - 70), w: 260, h: 140, z: 0,
  }
  doc.nodes.push(n)
  selectionIds.value = [n.id]
  pushHistory()
}

function addImageNode(src, prompt = '') {
  const c = viewportCenterWorld()
  const n = {
    id: newId('i'), type: 'image', src, prompt,
    x: Math.round(c.x - 256 + cascade()), y: Math.round(c.y - 256 + cascade()),
    w: 512, h: 512, z: 0,
  }
  doc.nodes.push(n)
  selectionIds.value = [n.id]
  pushHistory()
}

let cascadeN = 0
function cascade() {
  cascadeN = (cascadeN + 1) % 8
  return cascadeN * 24
}

function viewportCenterWorld() {
  const vp = stage.value?.vp
  const el = document.querySelector('.cv-stage')
  const w = el ? el.clientWidth : 800
  const h = el ? el.clientHeight : 600
  if (!vp) return { x: 0, y: 0 }
  return screenToWorld(w / 2, h / 2, vp)
}

function deleteSelection() {
  if (!selectionIds.value.length) return
  const dead = new Set(selectionIds.value)
  doc.nodes = doc.nodes.filter((n) => !dead.has(n.id))
  doc.edges = doc.edges.filter((e) => !dead.has(e.from) && !dead.has(e.to))
  selectionIds.value = []
  pushHistory()
}

// ---- 画布管理 ----

async function switchCanvas(id) {
  if (id === currentId.value) return
  await saveNow()
  loading.value = true
  try {
    await loadCanvas(id)
  } catch (e) {
    loadError.value = String(e.message || e)
  } finally {
    loading.value = false
  }
}

async function newCanvas() {
  await saveNow()
  const r = await api('/canvas', jsonBody('POST', { title: `画布 ${canvases.value.length + 1}` }))
  if (!r.ok) return
  await loadList()
  await switchCanvas(r.data.id)
}

async function renameCanvas() {
  const t = window.prompt('画布标题', title.value)
  if (t === null) return
  title.value = (t.trim() || '未命名画布').slice(0, 120)
  await saveNow()
}

async function deleteCanvas() {
  if (!window.confirm(`删除画布「${title.value}」?此操作不可撤销。`)) return
  const r = await api(`/canvas/${currentId.value}`, { method: 'DELETE' })
  if (!r.ok) return
  await loadList()
  if (canvases.value.length) {
    await loadCanvas(canvases.value[0].id)
  } else {
    const c = await api('/canvas', jsonBody('POST', { title: '我的画布' }))
    await loadList()
    if (c.ok) await loadCanvas(c.data.id)
  }
}

// ---- 我的图片 ----

async function openPicker() {
  pickerOpen.value = true
  if (pickerItems.value.length) return
  pickerLoading.value = true
  const r = await api('/my-images')
  pickerItems.value = (r.data?.data || []).filter((f) =>
    /\.(png|jpe?g|webp|gif)$/i.test(f.name || ''),
  )
  pickerLoading.value = false
}

// ================= 生成 =================

async function refToBase64(name) {
  const res = await fetch(generatedUrl(name))
  if (!res.ok) throw new Error('fetch ref failed')
  const blob = await res.blob()
  const dataUrl = await new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(fr.result)
    fr.onerror = reject
    fr.readAsDataURL(blob)
  })
  return dataUrl.replace(/^data:[^,]*,/, '')
}

async function generate() {
  if (genBusy.value || !model.value) return
  const prompt = (gen.prompt || '').trim() || selectedNotes.value.map((n) => n.text.trim()).join('\n')
  if (!prompt) {
    genMsg.value = '请输入提示词,或选中一个有内容的笔记节点'
    return
  }
  genBusy.value = true
  genMsg.value = ''

  const size = ratioToSize(gen.ratio || ratios.value[0])
  const sources = [
    ...selectedNotes.value,
    ...selectedImages.value.slice(0, 4),
  ]
  // 落点:参与源节点的包围盒右侧;无源则视口中心
  let x
  let y
  if (sources.length) {
    let maxX = -Infinity
    let minY = Infinity
    for (const s of sources) {
      maxX = Math.max(maxX, s.x + s.w)
      minY = Math.min(minY, s.y)
    }
    x = maxX + 64
    y = minY
  } else {
    const c = viewportCenterWorld()
    x = c.x - size.w / 2
    y = c.y - size.h / 2
  }

  const node = {
    id: newId('g'), type: 'gen', status: 'running',
    x: Math.round(x), y: Math.round(y), w: size.w, h: size.h, z: 0,
  }
  doc.nodes.push(node)
  for (const s of sources) {
    doc.edges.push({ id: newId('e'), from: s.id, to: node.id })
  }
  selectionIds.value = [node.id]
  pushHistory()

  try {
    const payload = { model: gen.modelId, prompt, ratio: gen.ratio || ratios.value[0] }
    if (selectedImages.value.length) {
      payload.reference_images = await Promise.all(
        selectedImages.value.slice(0, 4).map((n) => refToBase64(n.src).catch(() => null)),
      ).then((arr) => arr.filter(Boolean))
    }
    const opts = jsonBody('POST', payload)
    opts.headers['Idempotency-Key'] = node.id
    const r = await api('/generate', opts)
    if (r.ok && r.data?.url) {
      node.type = 'image'
      node.src = r.data.url.replace(/^\/images\//, '')
      node.prompt = prompt
      node.model = model.value.alias || model.value.name || model.value.id
      delete node.status
      delete node.error
      if (auth.user && r.data.credits != null) auth.user.credits = r.data.credits
      genMsg.value = `完成 · 扣 ${r.data.charged ?? '?'} 积分 · ${r.data.elapsed_ms ?? '?'}ms`
      pushHistory()
    } else if (r.status === 409 || [502, 504, 522, 524].includes(r.status)) {
      // 网关重发/超时:后端可能仍在生成。节点保持"生成中",用户可稍后删除。
      genMsg.value = '请求超时或重复提交,任务可能仍在后台执行'
    } else {
      node.status = 'failed'
      node.error = r.data?.detail || `失败 (${r.status})`
      genMsg.value = node.error
      pushHistory()
    }
  } catch (e) {
    node.status = 'failed'
    node.error = String(e)
    genMsg.value = String(e)
    pushHistory()
  } finally {
    genBusy.value = false
  }
}

// ================= 舞台事件 =================

function onSelection(ids) {
  selectionIds.value = ids
}
function onMoveEnd() {
  pushHistory()
}
function onNoteCommit({ id, text }) {
  const n = doc.nodes.find((x) => x.id === id)
  if (!n) return
  n.text = text
  pushHistory()
}
function onImgSize({ id, w, h }) {
  const n = doc.nodes.find((x) => x.id === id)
  if (!n || w <= 0 || h <= 0) return
  if (Math.abs(n.w - w) <= 1 && Math.abs(n.h - h) <= 1) return
  // 保持中心不动的尺寸校正
  const cx = n.x + n.w / 2
  const cy = n.y + n.h / 2
  n.w = w
  n.h = h
  n.x = Math.round(cx - w / 2)
  n.y = Math.round(cy - h / 2)
  markDirty() // 尺寸校正不进撤销历史(自动行为)
}
function onViewportRest(v) {
  if (
    doc.viewport.x !== v.x || doc.viewport.y !== v.y || doc.viewport.scale !== v.scale
  ) {
    doc.viewport = { ...v }
    markDirty()
  }
}
function onPanZoom(v) {
  scalePct.value = Math.round(v.scale * 100)
}
function onNodeAction({ type, id }) {
  if (type !== 'preview') return
  const n = doc.nodes.find((x) => x.id === id)
  if (n?.type === 'image') previewSrc.value = n.src
}

// ================= 快捷键 =================

function isTyping(t) {
  return !!(t && t.closest && t.closest('input, textarea, select, [contenteditable="true"]'))
}

function onKeydown(e) {
  if (isTyping(e.target)) {
    if (e.key === 'Escape') e.target.blur()
    return
  }
  const mod = e.ctrlKey || e.metaKey
  if (mod && e.key.toLowerCase() === 'z') {
    e.preventDefault()
    if (e.shiftKey) redo()
    else undo()
  } else if (mod && e.key.toLowerCase() === 'y') {
    e.preventDefault()
    redo()
  } else if (mod && e.key.toLowerCase() === 'a') {
    e.preventDefault()
    stage.value?.selectAll()
  } else if (e.key === 'Delete' || e.key === 'Backspace') {
    e.preventDefault()
    deleteSelection()
  } else if (e.key === 'Escape') {
    stage.value?.clearSelection()
    previewSrc.value = ''
    pickerOpen.value = false
  } else if (e.key === '+' || e.key === '=') {
    stage.value?.zoomBy(1.2)
  } else if (e.key === '-') {
    stage.value?.zoomBy(1 / 1.2)
  } else if (e.key === '0') {
    stage.value?.setViewport({ x: 0, y: 0, scale: 1 })
  }
}

function selectAll() {
  stage.value?.selectAll()
}

// ================= 生命周期 =================

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('beforeunload', flushSync)
  document.addEventListener('visibilitychange', onVisibility)
  const mm = await api('/managed-models')
  allModels.value = mm.data?.data || []
  const first = imageModels.value[0]
  if (first) {
    gen.modelId = first.id
    gen.ratio = (first.ratios && first.ratios[0]) || '1:1'
  }
  await boot()
})

function onVisibility() {
  if (document.visibilityState === 'hidden') flushSync()
}

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('beforeunload', flushSync)
  document.removeEventListener('visibilitychange', onVisibility)
  clearTimeout(saveTimer)
  clearTimeout(retryTimer)
})

// 路由离开前确保保存(组件级守卫,避免全局污染)
onBeforeRouteLeave(async () => {
  if (saveState.value === 'dirty' || saveState.value === 'error') await saveNow()
  return true
})

const saveLabel = computed(() => ({
  saved: '已保存', dirty: '待保存…', saving: '保存中…', error: '保存失败,重试中',
}[saveState.value] || ''))
</script>

<template>
  <div class="cv-page">
    <!-- 顶栏 -->
    <div class="cv-top">
      <div class="cv-top__left">
        <button class="cv-btn" title="返回" @click="$router.push('/user')">←</button>
        <select
          class="cv-select"
          :value="currentId"
          @change="switchCanvas($event.target.value)"
        >
          <option v-for="c in canvases" :key="c.id" :value="c.id">{{ c.title }}</option>
        </select>
        <button class="cv-btn" title="重命名" @click="renameCanvas">✎</button>
        <button class="cv-btn" title="新建画布" @click="newCanvas">＋</button>
        <button class="cv-btn cv-btn--danger" title="删除画布" @click="deleteCanvas">🗑</button>
      </div>

      <div class="cv-top__right">
        <button class="cv-btn" :disabled="!undoCount" title="撤销 (Ctrl+Z)" @click="undo">↩</button>
        <button class="cv-btn" :disabled="!redoCount" title="重做 (Ctrl+Shift+Z)" @click="redo">↪</button>
        <span class="cv-sep" />
        <button class="cv-btn" title="缩小" @click="stage?.zoomBy(1 / 1.2)">−</button>
        <span class="cv-zoom">{{ scalePct }}%</span>
        <button class="cv-btn" title="放大" @click="stage?.zoomBy(1.2)">＋</button>
        <button class="cv-btn" title="重置视图" @click="stage?.setViewport({ x: 0, y: 0, scale: 1 })">⌂</button>
        <span class="cv-sep" />
        <button class="cv-btn" @click="addNote">＋ 笔记</button>
        <button class="cv-btn" @click="openPicker">＋ 图片</button>
        <span class="cv-save" :data-state="saveState">{{ saveLabel }}</span>
      </div>
    </div>

    <!-- 舞台 + 生成面板 -->
    <div class="cv-body">
      <div class="cv-stagewrap">
        <div v-if="loading" class="cv-loading">加载中…</div>
        <div v-else-if="loadError" class="cv-error">{{ loadError }} <button class="cv-btn" @click="boot">重试</button></div>
        <CanvasStage
          v-else
          ref="stage"
          :nodes="doc.nodes"
          :edges="doc.edges"
          @selection="onSelection"
          @move-end="onMoveEnd"
          @note-commit="onNoteCommit"
          @img-size="onImgSize"
          @viewport-rest="onViewportRest"
          @pan-zoom="onPanZoom"
          @node-action="onNodeAction"
        />
      </div>

      <!-- 生成面板 -->
      <aside class="cv-panel">
        <div class="cv-panel__title">生成到画布</div>
        <label class="cv-label">模型</label>
        <select v-model="gen.modelId" class="cv-input" @change="gen.ratio = ratios[0]">
          <option v-for="m in imageModels" :key="m.id" :value="m.id">
            {{ m.alias || m.name || m.id }}
          </option>
        </select>

        <label class="cv-label">比例</label>
        <div class="cv-ratios">
          <button
            v-for="r in ratios"
            :key="r"
            type="button"
            class="cv-chip"
            :class="{ 'is-on': gen.ratio === r }"
            @click="gen.ratio = r"
          >{{ r }}</button>
        </div>

        <label class="cv-label">提示词(留空则使用选中的笔记)</label>
        <textarea
          v-model="gen.prompt"
          class="cv-input cv-textarea"
          rows="4"
          placeholder="描述你要生成的画面…"
        />

        <div class="cv-refs">
          参考图:选中画布上的图片节点({{ selectedImages.length }}/4)
        </div>

        <button class="cv-gen" :disabled="genBusy || !model" @click="generate">
          {{ genBusy ? '生成中…' : '生成' }}
        </button>
        <div v-if="genMsg" class="cv-genmsg">{{ genMsg }}</div>

        <div class="cv-tips">
          双击笔记可编辑;Delete 删除选中;Ctrl+Z 撤销;
          生成结果会自动连线到来源节点。
        </div>
      </aside>
    </div>

    <!-- 图片预览 -->
    <div v-if="previewSrc" class="cv-lightbox" @click="previewSrc = ''">
      <img :src="generatedUrl(previewSrc)" alt="" @click.stop>
    </div>

    <!-- 我的图片选择 -->
    <div v-if="pickerOpen" class="cv-picker" @click.self="pickerOpen = false">
      <div class="cv-picker__box">
        <div class="cv-picker__head">
          <span>插入图片(点击放置到画布)</span>
          <button class="cv-btn" @click="pickerOpen = false">✕</button>
        </div>
        <div v-if="pickerLoading" class="cv-loading">加载中…</div>
        <div v-else-if="!pickerItems.length" class="cv-loading">还没有生成过图片</div>
        <div v-else class="cv-picker__grid">
          <img
            v-for="f in pickerItems"
            :key="f.name"
            :src="thumbUrl(f.name)"
            loading="lazy"
            alt=""
            @click="addImageNode(f.name); pickerOpen = false"
          >
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cv-btn {
  padding: 5px 10px;
  border-radius: 8px;
  border: 1px solid rgb(255 255 255 / 0.14);
  background: rgb(255 255 255 / 0.04);
  color: #c6cbd8;
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
}
.cv-btn:hover:not(:disabled) {
  background: rgb(255 255 255 / 0.09);
  color: #fff;
}
.cv-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.cv-btn--danger:hover:not(:disabled) {
  border-color: #6b3b44;
  color: #e58c96;
}
.cv-select,
.cv-input {
  padding: 6px 9px;
  border-radius: 8px;
  border: 1px solid rgb(255 255 255 / 0.14);
  background: #0e1015;
  color: #e8eaf0;
  font-size: 13px;
  outline: none;
}
.cv-select:focus,
.cv-input:focus {
  border-color: #7c6cff;
}

.cv-page {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  flex-direction: column;
  background: #0e1015;
}
.cv-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid rgb(255 255 255 / 0.08);
  background: #12141b;
  flex-wrap: wrap;
}
.cv-top__left,
.cv-top__right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.cv-select {
  max-width: 220px;
}
.cv-sep {
  width: 1px;
  height: 18px;
  background: rgb(255 255 255 / 0.12);
  margin: 0 4px;
}
.cv-zoom {
  font-size: 12px;
  color: #9aa1b2;
  min-width: 44px;
  text-align: center;
}
.cv-save {
  font-size: 11px;
  color: #7f8798;
  margin-left: 8px;
}
.cv-save[data-state='error'] {
  color: #e58c96;
}
.cv-save[data-state='saved'] {
  color: #6fae8c;
}

.cv-body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.cv-stagewrap {
  position: relative;
  flex: 1;
  min-width: 0;
}
.cv-loading,
.cv-error {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #9aa1b2;
  background: #0e1015;
  z-index: 5;
}
.cv-error {
  color: #e58c96;
}

.cv-panel {
  width: 260px;
  flex-shrink: 0;
  border-left: 1px solid rgb(255 255 255 / 0.08);
  background: #12141b;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
}
.cv-panel__title {
  font-size: 14px;
  font-weight: 600;
  color: #e8eaf0;
}
.cv-label {
  font-size: 11px;
  color: #7f8798;
  margin-top: 4px;
}
.cv-input {
  width: 100%;
}
.cv-textarea {
  resize: vertical;
  min-height: 72px;
}
.cv-ratios {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.cv-chip {
  padding: 3px 10px;
  border-radius: 999px;
  border: 1px solid rgb(255 255 255 / 0.14);
  background: transparent;
  color: #b6bccb;
  font-size: 12px;
  cursor: pointer;
}
.cv-chip.is-on {
  border-color: #7c6cff;
  color: #a99dff;
  background: rgb(124 108 255 / 0.12);
}
.cv-refs {
  font-size: 11px;
  color: #7f8798;
  border: 1px dashed rgb(255 255 255 / 0.14);
  border-radius: 8px;
  padding: 8px 10px;
}
.cv-gen {
  padding: 9px;
  border-radius: 9px;
  border: none;
  background: linear-gradient(135deg, #7c6cff, #5d8bff);
  color: #fff;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}
.cv-gen:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.cv-genmsg {
  font-size: 11px;
  color: #9aa1b2;
  word-break: break-all;
}
.cv-tips {
  font-size: 11px;
  color: #5f6675;
  line-height: 1.6;
  margin-top: auto;
}

.cv-lightbox {
  position: fixed;
  inset: 0;
  background: rgb(4 5 8 / 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  cursor: zoom-out;
}
.cv-lightbox img {
  max-width: 92vw;
  max-height: 92vh;
  border-radius: 8px;
}

.cv-picker {
  position: fixed;
  inset: 0;
  background: rgb(4 5 8 / 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 90;
}
.cv-picker__box {
  width: min(720px, 92vw);
  max-height: 80vh;
  background: #161922;
  border: 1px solid rgb(255 255 255 / 0.1);
  border-radius: 12px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.cv-picker__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  color: #e8eaf0;
}
.cv-picker__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 8px;
  overflow-y: auto;
}
.cv-picker__grid img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 8px;
  cursor: pointer;
  background: #0e1015;
}
.cv-picker__grid img:hover {
  outline: 2px solid #7c6cff;
}
</style>
