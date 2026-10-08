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
const fileInput = ref(null)
const uploadBusy = ref(false)
const dropHover = ref(false)

// ================= 文档加载 / 保存 =================

async function loadList() {
  const r = await api('/canvas')
  if (!r.ok) throw new Error(r.data?.detail || '加载画布列表失败')
  canvases.value = r.data?.data || []
}

// ---- 本地缓存(localStorage):打开画布先渲染缓存秒开,再后台对齐服务器 ----
const CV_CACHE_PREFIX = 'cv:cache:'

function readCache(id) {
  try {
    const raw = localStorage.getItem(CV_CACHE_PREFIX + id)
    if (!raw) return null
    const c = JSON.parse(raw)
    if (typeof c.data !== 'string') return null
    return { title: typeof c.title === 'string' ? c.title : '未命名画布', data: c.data }
  } catch {
    return null
  }
}
function writeCache(id) {
  if (!id) return
  try {
    localStorage.setItem(
      CV_CACHE_PREFIX + id,
      JSON.stringify({ title: title.value, data: serializeDoc(doc) }),
    )
  } catch { /* 配额满等异常:缓存是尽力而为,失败不影响功能 */ }
}
function clearCache(id) {
  try { localStorage.removeItem(CV_CACHE_PREFIX + id) } catch { /* 同上 */ }
}

function applyDoc(id, docTitle, d) {
  currentId.value = id
  title.value = docTitle || '未命名画布'
  doc.viewport = d.viewport
  doc.nodes = d.nodes
  doc.edges = d.edges
  selectionIds.value = []
  resetHistory()
  saveState.value = 'saved'
}

async function loadCanvas(id) {
  // 1) 本地缓存先行:立即渲染、立即可交互(离线也可用)
  let appliedFromCache = false
  const cached = readCache(id)
  if (cached) {
    const cd = normalizeDoc(safeParse(cached.data))
    for (const n of cd.nodes) {
      if (n.type === 'gen' && n.status === 'running') {
        n.status = 'failed'
        n.error = '页面已刷新,任务中断'
      }
    }
    applyDoc(id, cached.title, cd)
    appliedFromCache = true
    loading.value = false
    await nextTick()
    stage.value?.setViewport(cd.viewport)
    stage.value?.syncZTop()
  }

  // 2) 服务器版本对齐
  const r = await api(`/canvas/${id}`)
  if (!r.ok) {
    if (appliedFromCache) return // 拉取失败但缓存可用:继续用缓存
    throw new Error(r.data?.detail || '加载画布失败')
  }
  const serverData = typeof r.data?.data === 'string' ? r.data.data : serializeDoc(normalizeDoc(safeParse(r.data?.data)))
  if (appliedFromCache && cached && cached.data === serverData && cached.title === (r.data.title || '未命名画布')) {
    writeCache(id) // 刷新缓存时间戳意义不大,但保持一致路径
    return // 内容一致:不重刷,保留撤销历史与当前交互
  }
  const d = normalizeDoc(safeParse(r.data?.data))
  // 页面刷新会丢掉进行中的生成任务结果(无轮询),如实标记中断。
  for (const n of d.nodes) {
    if (n.type === 'gen' && n.status === 'running') {
      n.status = 'failed'
      n.error = '页面已刷新,任务中断'
    }
  }
  applyDoc(r.data.id, r.data.title, d)
  await nextTick()
  stage.value?.setViewport(d.viewport)
  stage.value?.syncZTop()
  writeCache(id)
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
      writeCache(currentId.value)
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
  addNoteAt(viewportCenterWorld())
}

function addNoteAt(w) {
  const n = {
    id: newId('t'), type: 'note', text: '',
    x: Math.round(w.x - 130), y: Math.round(w.y - 70), w: 260, h: 140, z: 0,
  }
  doc.nodes.push(n)
  selectionIds.value = [n.id]
  pushHistory()
}

function addImageNode(src, prompt = '', at = null) {
  const c = at || viewportCenterWorld()
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

// ---- 复制选中节点(带相互连线,偏移落位) ----
function duplicateSelection() {
  const sel = doc.nodes.filter((n) => selectionIds.value.includes(n.id) && n.type !== 'gen')
  if (!sel.length) return
  const idMap = {}
  const clones = sel.map((n) => {
    const c = {
      ...n,
      id: newId(n.type === 'note' ? 't' : 'i'),
      x: n.x + 36,
      y: n.y + 36,
    }
    idMap[n.id] = c.id
    return c
  })
  doc.nodes.push(...clones)
  for (const e of [...doc.edges]) {
    if (idMap[e.from] && idMap[e.to]) {
      doc.edges.push({ id: newId('e'), from: idMap[e.from], to: idMap[e.to] })
    }
  }
  selectionIds.value = clones.map((c) => c.id)
  stage.value?.selectIds?.(selectionIds.value)
  pushHistory()
}

function deleteSelection() {
  const dead = new Set(selectionIds.value)
  const deadEdges = stage.value?.consumeSelectedEdges() || []
  if (!dead.size && !deadEdges.length) return
  if (dead.size) {
    doc.nodes = doc.nodes.filter((n) => !dead.has(n.id))
    doc.edges = doc.edges.filter((e) => !dead.has(e.from) && !dead.has(e.to))
  }
  if (deadEdges.length) {
    const gone = new Set(deadEdges)
    doc.edges = doc.edges.filter((e) => !gone.has(e.id))
  }
  selectionIds.value = []
  pushHistory()
}

// ---- 手动连线 ----
function onLinkCreate({ from, to }) {
  if (doc.edges.some((e) => e.from === from && e.to === to)) return // 去重
  doc.edges.push({ id: newId('e'), from, to })
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
  clearCache(currentId.value)
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

// ---- 上传本地图片(按钮/拖拽/粘贴三条路径都汇到这里) ----
async function uploadImageFile(file, at = null) {
  if (!file || !file.type?.startsWith('image/')) return
  uploadBusy.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    const r = await api('/my-images', { method: 'POST', body: fd })
    if (r.ok && r.data?.name) {
      pickerItems.value.unshift({ name: r.data.name })
      addImageNode(r.data.name, '', at)
    } else {
      genMsg.value = r.data?.detail || `上传失败 (${r.status})`
    }
  } catch (e) {
    genMsg.value = String(e)
  } finally {
    uploadBusy.value = false
  }
}

function onFilePicked(e) {
  const files = [...(e.target.files || [])]
  e.target.value = ''
  files.forEach((f) => uploadImageFile(f))
}

// 拖拽本地图片落到画布:落点即拖放位置
function onDrop(e) {
  dropHover.value = false
  const files = [...(e.dataTransfer?.files || [])].filter((f) => f.type?.startsWith('image/'))
  if (!files.length) return
  const el = document.querySelector('.cv-stage')
  const rect = el?.getBoundingClientRect()
  const vp = stage.value?.vp
  const at =
    rect && vp ? screenToWorld(e.clientX - rect.left, e.clientY - rect.top, vp) : null
  files.forEach((f) => uploadImageFile(f, at))
}

function onPaste(e) {
  if (isTyping(e.target)) return
  for (const it of e.clipboardData?.items || []) {
    if (it.type?.startsWith('image/')) {
      const f = it.getAsFile()
      if (f) {
        e.preventDefault()
        uploadImageFile(f)
        return
      }
    }
  }
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
  const n = doc.nodes.find((x) => x.id === id)
  if (!n) return
  if (type === 'preview') {
    if (n.type === 'image') previewSrc.value = n.src
  } else if (type === 'duplicate') {
    selectionIds.value = [id]
    duplicateSelection()
  } else if (type === 'delete') {
    selectionIds.value = [id]
    deleteSelection()
  } else if (type === 'front') {
    stage.value?.bumpNode?.(id)
    markDirty()
  }
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
  } else if (mod && e.key.toLowerCase() === 'd') {
    e.preventDefault()
    duplicateSelection()
  } else if (e.key === 'Delete' || e.key === 'Backspace') {
    e.preventDefault()
    deleteSelection()
  } else if (e.key === 'Escape') {
    stage.value?.cancelLink()
    stage.value?.clearSelection()
    previewSrc.value = ''
    pickerOpen.value = false
  } else if (e.key === '+' || e.key === '=') {
    stage.value?.zoomBy(1.2)
  } else if (e.key === '-') {
    stage.value?.zoomBy(1 / 1.2)
  } else if (e.key === '0') {
    stage.value?.setViewport({ x: 0, y: 0, scale: 1 })
  } else if (e.key === '1') {
    stage.value?.fitToContent?.()
  }
}

function selectAll() {
  stage.value?.selectAll()
}

// ================= 生命周期 =================

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('paste', onPaste)
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
  window.removeEventListener('paste', onPaste)
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
  <!-- 传送到 body:布局 <main> 自带 relative z-10 层叠上下文,留在里面会被
       站点固定顶栏(z-40)盖住,画布工具栏无法点击。 -->
  <Teleport to="body">
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
        <button class="cv-btn" title="重置视图 (0)" @click="stage?.setViewport({ x: 0, y: 0, scale: 1 })">⌂</button>
        <button class="cv-btn" title="适应内容 (1)" @click="stage?.fitToContent?.()">⤢</button>
        <span class="cv-sep" />
        <button class="cv-btn cv-btn--accent" @click="addNote">＋ 笔记</button>
        <button class="cv-btn cv-btn--accent" @click="openPicker">＋ 图片</button>
        <button class="cv-btn" :disabled="uploadBusy" title="上传本地图片(也可直接拖入/粘贴)" @click="fileInput?.click()">
          {{ uploadBusy ? '上传中…' : '⤒ 上传' }}
        </button>
        <input ref="fileInput" type="file" accept="image/*" multiple hidden @change="onFilePicked">
        <span class="cv-save" :data-state="saveState">{{ saveLabel }}</span>
      </div>
    </div>

    <!-- 舞台 + 生成面板 -->
    <div class="cv-body">
      <div
        class="cv-stagewrap"
        @dragover.prevent="dropHover = true"
        @dragleave="dropHover = false"
        @drop.prevent="onDrop"
      >
        <div v-if="dropHover" class="cv-drop">松开把图片放进画布</div>
        <div v-if="loading" class="cv-loading">加载中…</div>
        <div v-else-if="loadError" class="cv-error">{{ loadError }} <button class="cv-btn" @click="boot">重试</button></div>
        <CanvasStage
          v-else
          ref="stage"
          :nodes="doc.nodes"
          :edges="doc.edges"
          @selection="onSelection"
          @move-end="onMoveEnd"
          @link-create="onLinkCreate"
          @note-commit="onNoteCommit"
          @img-size="onImgSize"
          @viewport-rest="onViewportRest"
          @pan-zoom="onPanZoom"
          @node-action="onNodeAction"
          @blank-dblclick="addNoteAt"
        />
      </div>

      <!-- 生成面板 -->
      <aside class="cv-panel">
        <div class="cv-panel__title">生成到画布</div>
        <label class="cv-label">模型</label>
        <select v-if="imageModels.length" v-model="gen.modelId" class="cv-input" @change="gen.ratio = ratios[0]">
          <option v-for="m in imageModels" :key="m.id" :value="m.id">
            {{ m.alias || m.name || m.id }}
          </option>
        </select>
        <div v-else class="cv-nomodels">
          ⚠ 站点还没有可用的图像模型。请管理员到「管理后台 → 模型管理」添加并启用模型后,这里就能选择了。
        </div>

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
          双击空白建笔记 · 拖右侧圆点连线 · 选中后右下角缩放 · Ctrl+D 复制 ·
          本地图片直接拖入或 Ctrl+V 粘贴 · 按 1 适应全部内容
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
          <div class="cv-picker__acts">
            <button class="cv-btn cv-btn--accent" @click="fileInput?.click()">⤒ 上传本地</button>
            <button class="cv-btn" @click="pickerOpen = false">✕</button>
          </div>
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
  </Teleport>
</template>

<style scoped>
/* ===== 画布:Memphis/neo-brutalism 全屏工作台(随站点明暗主题走变量) ===== */
.cv-btn {
  padding: 5px 11px;
  border-radius: 9px;
  border: 2.5px solid var(--ink);
  background: var(--surface);
  color: var(--fg);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  box-shadow: 3px 3px 0 var(--shadow-ink);
  transition: transform 0.12s ease, box-shadow 0.12s ease, background 0.12s ease;
}
.cv-btn:hover:not(:disabled) {
  background: var(--m-yellow);
}
.cv-btn:active:not(:disabled) {
  transform: translate(2px, 2px);
  box-shadow: 1px 1px 0 var(--shadow-ink);
}
.cv-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.cv-btn--danger:hover:not(:disabled) {
  background: #ffb4ab;
}
.cv-btn--accent {
  background: var(--m-blue);
}
.cv-btn--accent:hover:not(:disabled) {
  background: var(--m-mint);
}
.cv-select,
.cv-input {
  padding: 6px 10px;
  border-radius: 9px;
  border: 2.5px solid var(--ink);
  background: var(--surface);
  color: var(--fg);
  font-size: 13px;
  outline: none;
}
.cv-select:focus,
.cv-input:focus {
  box-shadow: 3px 3px 0 var(--m-orange);
}

.cv-page {
  position: fixed;
  inset: 0;
  /* 站点顶栏是 z-40;画布全屏接管时必须盖住它 */
  z-index: 50;
  display: flex;
  flex-direction: column;
  background: var(--app-bg);
}
.cv-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 14px;
  border-bottom: 3px solid var(--ink);
  background: var(--surface);
  flex-wrap: wrap;
}
.cv-top__left,
.cv-top__right {
  display: flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
}
.cv-select {
  max-width: 220px;
}
.cv-sep {
  width: 2.5px;
  height: 20px;
  border-radius: 2px;
  background: var(--ink);
  opacity: 0.25;
  margin: 0 4px;
}
.cv-zoom {
  font-size: 12px;
  font-weight: 700;
  color: var(--fg);
  min-width: 46px;
  text-align: center;
  background: var(--m-yellow);
  border: 2px solid var(--ink);
  border-radius: 999px;
  padding: 1px 8px;
}
.cv-save {
  font-size: 11px;
  font-weight: 700;
  color: var(--fg-3);
  margin-left: 8px;
}
.cv-save[data-state='saved'] {
  color: #15803d;
}
.cv-save[data-state='dirty'],
.cv-save[data-state='saving'] {
  color: var(--m-orange);
}
.cv-save[data-state='error'] {
  color: #dc2626;
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
.cv-drop {
  position: absolute;
  inset: 10px;
  z-index: 6;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 15px;
  font-weight: 800;
  color: var(--fg);
  border: 3px dashed var(--ink);
  border-radius: 18px;
  background: color-mix(in srgb, var(--m-blue) 55%, transparent);
  pointer-events: none;
}
.cv-loading,
.cv-error {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--fg-2);
  font-weight: 600;
  background: var(--app-bg);
  z-index: 5;
}
.cv-error {
  color: #dc2626;
}

/* 右侧生成面板 = 白纸卡片 */
.cv-panel {
  width: 264px;
  flex-shrink: 0;
  border-left: 3px solid var(--ink);
  background: var(--surface);
  padding: 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 9px;
  overflow-y: auto;
}
.cv-panel__title {
  font-size: 15px;
  font-weight: 800;
  color: var(--fg);
  display: inline-block;
  background: linear-gradient(transparent 55%, var(--m-yellow) 55%);
  align-self: flex-start;
  padding: 0 2px;
}
.cv-label {
  font-size: 11px;
  font-weight: 700;
  color: var(--fg-3);
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
  padding: 3px 11px;
  border-radius: 999px;
  border: 2.5px solid var(--ink);
  background: var(--surface);
  color: var(--fg);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}
.cv-chip:hover {
  background: var(--m-blue);
}
.cv-chip.is-on {
  background: var(--m-yellow);
  box-shadow: 2.5px 2.5px 0 var(--shadow-ink);
}
.cv-refs {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  border: 2px dashed var(--ink-soft);
  border-radius: 9px;
  padding: 8px 10px;
}
.cv-gen {
  padding: 10px;
  border-radius: 11px;
  border: 2.5px solid var(--ink);
  background: var(--m-orange);
  color: #fff;
  font-size: 14px;
  font-weight: 800;
  cursor: pointer;
  box-shadow: 4px 4px 0 var(--shadow-ink);
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}
.cv-gen:hover:not(:disabled) {
  transform: translate(-1px, -1px);
  box-shadow: 5px 5px 0 var(--shadow-ink);
}
.cv-gen:active:not(:disabled) {
  transform: translate(3px, 3px);
  box-shadow: 1px 1px 0 var(--shadow-ink);
}
.cv-gen:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.cv-genmsg {
  font-size: 11px;
  color: var(--fg-2);
  word-break: break-all;
}
.cv-nomodels {
  font-size: 11px;
  font-weight: 600;
  color: #92400e;
  line-height: 1.6;
  border: 2px dashed #f59e0b;
  border-radius: 9px;
  background: var(--m-yellow);
  padding: 8px 10px;
}
.cv-tips {
  font-size: 11px;
  color: var(--fg-faint);
  line-height: 1.7;
  margin-top: auto;
}

.cv-lightbox {
  position: fixed;
  inset: 0;
  background: rgb(17 17 17 / 0.75);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  cursor: zoom-out;
}
.cv-lightbox img {
  max-width: 92vw;
  max-height: 92vh;
  border-radius: 12px;
  border: 3px solid #fff;
  box-shadow: 10px 10px 0 var(--m-orange);
}

.cv-picker {
  position: fixed;
  inset: 0;
  background: rgb(17 17 17 / 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 90;
}
.cv-picker__box {
  width: min(720px, 92vw);
  max-height: 80vh;
  background: var(--surface);
  border: 3px solid var(--ink);
  border-radius: 16px;
  box-shadow: 8px 8px 0 var(--shadow-ink);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.cv-picker__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 800;
  color: var(--fg);
}
.cv-picker__acts {
  display: flex;
  gap: 8px;
}
.cv-picker__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 10px;
  overflow-y: auto;
}
.cv-picker__grid img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 10px;
  border: 2.5px solid var(--ink);
  cursor: pointer;
  background: var(--surface-2);
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}
.cv-picker__grid img:hover {
  transform: translate(-2px, -2px);
  box-shadow: 4px 4px 0 var(--m-orange);
}
</style>
