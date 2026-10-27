<script setup>
// 无限画布页面:文档管理(多画布/自动保存)、生成面板、撤销重做、快捷键。
// 交互细节全部在 CanvasStage;这里只做数据编排。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { api, jsonBody, loadImageBlob, loadImageData } from '../api'
import { auth } from '../auth'
import { normalizeDoc, newId, ratioToSize, serializeDoc, screenToWorld, rectsIntersect, DEFAULT_SCALE } from '../utils/canvas'
import CanvasStage from '../components/canvas/CanvasStage.vue'
import Icon from '../components/Icon.vue'
import SelectMenu from '../components/SelectMenu.vue'

const stage = ref(null)

// ---- 文档状态 ----
const canvases = ref([])
const currentId = ref('')
const title = ref('未命名画布')
const doc = reactive({ viewport: { x: 0, y: 0, scale: DEFAULT_SCALE }, nodes: [], edges: [] })
const loading = ref(true)
const loadError = ref('')
const saveState = ref('saved') // saved | dirty | saving | error
const scalePct = ref(100)

// ---- 选择 / 预览 ----
const selectionIds = ref([])
const previewSrc = ref('')
const previewUrl = ref('')
watch(previewSrc, async (v) => {
  if (previewUrl.value) { URL.revokeObjectURL(previewUrl.value); previewUrl.value = '' }
  if (!v) return
  try {
    const { url } = await loadImageBlob(v)
    if (previewSrc.value === v) previewUrl.value = url
  } catch { /* 预览失败保持空白遮罩 */ }
})
onBeforeUnmount(() => {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
})

// ---- 生成面板 ----
const allModels = ref([])
const gen = reactive({ kind: 'image', modelId: '', prompt: '', ratio: '', resolution: '', duration: '', count: 1 })
const runningGens = ref(0)
const genBusy = computed(() => runningGens.value > 0)
const genMsg = ref('')
// 消息只做短暂 toast 提示,自动消失不常驻
let genMsgTimer = null
watch(genMsg, (v) => {
  clearTimeout(genMsgTimer)
  if (v) genMsgTimer = setTimeout(() => { genMsg.value = '' }, 3500)
})
const kindModels = computed(() =>
  (allModels.value || []).filter((m) => m.sellable && m.type === gen.kind),
)
const model = computed(() => kindModels.value.find((m) => m.id === gen.modelId) || null)
const ratios = computed(() => model.value?.ratios?.length ? model.value.ratios : ['1:1'])
const resolutions = computed(() => model.value?.resolutions?.length
  ? model.value.resolutions
  : Object.keys(model.value?.prices || {}))
const durations = computed(() => model.value?.durations?.length ? model.value.durations : ['5s'])
// SelectMenu options(统一全站自定义下拉样式)
const kindOptions = [
  { value: 'image', label: '生图' },
  { value: 'video', label: '生视频' },
]
const modelOptions = computed(() =>
  kindModels.value.map((m) => ({ value: m.id, label: m.alias || m.name || m.id })),
)
const ratioOptions = computed(() => ratios.value.map((r) => ({ value: r, label: r })))
const resolutionOptions = computed(() => resolutions.value.map((r) => ({ value: r, label: r })))
const durationOptions = computed(() => durations.value.map((d) => ({ value: d, label: d })))
const countOptions = [1, 2, 3, 4].map((n) => ({ value: n, label: `${n} 张` }))
const canvasOptions = computed(() =>
  canvases.value.map((c) => ({ value: c.id, label: c.title || '未命名画布' })),
)
// 参考文件分类上限:custom 模型直接读模型三个字段;老模型只有
// max_reference_images(各类合计)或 image_to_image 兜底 1 张图。
const refCaps = computed(() => {
  const m = model.value || {}
  const imgs = Number(m.max_reference_images || 0) || (m.image_to_image ? 1 : 0)
  const vids = Number(m.max_reference_videos || 0)
  const auds = Number(m.max_reference_audios || 0)
  return { imgs, vids, auds, total: imgs + vids + auds }
})
const maxRefs = computed(() => Math.min(20, refCaps.value.total))
const supportsMediaRefs = computed(() => refCaps.value.vids > 0 || refCaps.value.auds > 0)
const refFileAccept = computed(() => (supportsMediaRefs.value ? 'image/*,video/*,audio/*' : 'image/*'))
const refCounts = computed(() => {
  const imgs = refImages.value.filter((f) => !f.fileType || f.fileType === 'image').length
  const vids = refImages.value.filter((f) => f.fileType === 'video').length
  const auds = refImages.value.filter((f) => f.fileType === 'audio').length
  return { imgs, vids, auds }
})
// 换类型/模型时重置参数默认值(和画图台一致)
watch(() => gen.kind, () => {
  const first = kindModels.value[0]
  gen.modelId = first?.id || ''
})
watch(model, (m) => {
  if (!m) return
  gen.ratio = m.ratios?.[0] || '1:1'
  const res = m.resolutions?.length ? m.resolutions : Object.keys(m.prices || {})
  gen.resolution = res[0] || '1K'
  gen.duration = m.durations?.[0] || '5s'
})
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

// 自动找空位:从锚点(默认视口中心)开始往右排,一行最多10个,超了才换下一行
const NODE_PAD = 48
const ROW_MAX = 10
function spotIsFree(x, y, w, h) {
  const r = { x: x - NODE_PAD / 2, y: y - NODE_PAD / 2, w: w + NODE_PAD, h: h + NODE_PAD }
  return !doc.nodes.some((n) => rectsIntersect(r, { x: n.x, y: n.y, w: n.w, h: n.h }))
}
function freeSpot(w, h, at = null) {
  const c = at || viewportCenterWorld()
  const x0 = Math.round(c.x - w / 2)
  const y0 = Math.round(c.y - h / 2)
  for (let i = 0; i < 200; i++) {
    const x = x0 + (i % ROW_MAX) * (w + NODE_PAD)
    const y = y0 + Math.floor(i / ROW_MAX) * (h + NODE_PAD)
    if (spotIsFree(x, y, w, h)) return { x, y }
  }
  return { x: x0, y: y0 }
}

function addNote() {
  const s = freeSpot(260, 140)
  const n = {
    id: newId('t'), type: 'note', text: '',
    x: s.x, y: s.y, w: 260, h: 140, z: 0,
  }
  doc.nodes.push(n)
  selectionIds.value = [n.id]
  pushHistory()
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
  // at 作为锚点(拖放位置);被占用就自动往右找空位,连续上传自然排成一排
  const s = freeSpot(512, 512, at)
  const n = {
    id: newId('i'), type: 'image', src, prompt,
    x: s.x, y: s.y, w: 512, h: 512, z: 0,
  }
  doc.nodes.push(n)
  selectionIds.value = [n.id]
  pushHistory()
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
      doc.edges.push({ ...e, id: newId('e'), from: idMap[e.from], to: idMap[e.to] })
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
function onLinkCreate({ from, to, fromSide, toSide }) {
  // 任意方向去重:两个节点之间只允许一条连线
  if (doc.edges.some((e) => (e.from === from && e.to === to) || (e.from === to && e.to === from))) return
  doc.edges.push({ id: newId('e'), from, to, fromSide, toSide })
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

const pickerUrls = reactive({}) // name -> blob URL(带鉴权加载)
async function ensurePickerThumb(name) {
  if (name in pickerUrls) return
  try {
    const { url } = await loadImageBlob(name)
    pickerUrls[name] = url
  } catch {
    pickerUrls[name] = ''
  }
}
async function openPicker() {
  pickerOpen.value = true
  if (pickerItems.value.length) return
  pickerLoading.value = true
  const r = await api('/my-images')
  pickerItems.value = (r.data?.data || []).filter((f) =>
    /\.(png|jpe?g|webp|gif)$/i.test(f.name || ''),
  )
  pickerItems.value.forEach((f) => ensurePickerThumb(f.name))
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
      ensurePickerThumb(r.data.name)
      addImageNode(r.data.name, '', at)
    } else {
      genMsg.value = r.data?.detail || `上传失败 (${r.status})`
    }
  } catch (e) {
    genMsg.value = e?.name === 'AbortError' ? '上传超时,请重试' : String(e)
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

// ---- 参考图上传(与画图台一致:直接传文件,不再用选中节点) ----
const refImages = ref([]) // { name, dataUrl }
const refInput = ref(null)
const MAX_REF_BYTES = 20 * 1024 * 1024

function fileToDataUrl(f) {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(fr.result)
    fr.onerror = reject
    fr.readAsDataURL(f)
  })
}
async function addRefFile(f) {
  const t = f?.type || ''
  const isImage = t.startsWith('image/')
  const isVideo = t.startsWith('video/')
  const isAudio = t.startsWith('audio/')
  if (!isImage && !(supportsMediaRefs.value && (isVideo || isAudio))) return
  if (refImages.value.length >= maxRefs.value) {
    genMsg.value = `参考文件最多 ${maxRefs.value} 个`
    return
  }
  if (f.size > MAX_REF_BYTES) {
    genMsg.value = '单个参考文件不能超过 20MB'
    return
  }
  const caps = refCaps.value
  const counts = refCounts.value
  const kind = isVideo ? 'video' : isAudio ? 'audio' : 'image'
  if (isVideo && counts.vids >= caps.vids) { genMsg.value = `视频参考最多 ${caps.vids} 个`; return }
  if (isAudio && counts.auds >= caps.auds) { genMsg.value = `音频参考最多 ${caps.auds} 个`; return }
  if (isImage && counts.imgs >= caps.imgs) { genMsg.value = `图片参考最多 ${caps.imgs} 张`; return }
  refImages.value.push({ name: f.name, dataUrl: await fileToDataUrl(f), fileType: kind })
}
// 画布图片节点 → 参考图(记录 nodeId,生成时自动连边标明来源)
async function addNodeAsRef(n) {
  if (!n || n.type !== 'image' || !n.src) return
  if (refImages.value.some((r) => r.nodeId === n.id)) return
  if (refImages.value.length >= maxRefs.value) {
    genMsg.value = `参考文件最多 ${maxRefs.value} 个`
    return
  }
  if (refCounts.value.imgs >= refCaps.value.imgs) {
    genMsg.value = `图片参考最多 ${refCaps.value.imgs} 张`
    return
  }
  try {
    const dataUrl = await loadImageData(n.src)
    refImages.value.push({ name: n.src, dataUrl, nodeId: n.id })
  } catch (e) {
    genMsg.value = `参考图读取失败:${e.message || e}`
  }
}
function onRefPicked(e) {
  const files = [...(e.target.files || [])]
  e.target.value = ''
  files.forEach(addRefFile)
}
function removeRef(i) {
  refImages.value.splice(i, 1)
}
watch(maxRefs, (n) => {
  if (refImages.value.length > n) refImages.value = refImages.value.slice(0, n)
})

async function generate() {
  if (!model.value) return
  const prompt = (gen.prompt || '').trim() || selectedNotes.value.map((n) => n.text.trim()).join('\n')
  if (!prompt) {
    genMsg.value = '请输入提示词,或选中一个有内容的笔记节点'
    return
  }
  if (refImages.value.length && !maxRefs.value) {
    genMsg.value = '当前模型不支持参考图，请切换模型'
    return
  }
  genMsg.value = ''

  const n = gen.kind === 'image' ? Math.max(1, Math.min(4, Number(gen.count) || 1)) : 1
  const size = ratioToSize(gen.ratio || ratios.value[0])
  // 生成来源:选中的笔记(提示词) + 通过 ＋ 添加为参考图的画布节点,生成时自动连线
  const refNodes = refImages.value
    .filter((r) => r.nodeId)
    .map((r) => doc.nodes.find((n) => n.id === r.nodeId))
    .filter(Boolean)
  const seenSrc = new Set()
  const sources = [...selectedNotes.value, ...refNodes].filter((n) => {
    if (seenSrc.has(n.id)) return false
    seenSrc.add(n.id)
    return true
  })
  // 落点:参与源节点的包围盒右侧;无源则视口中心。多张时横向排开。
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
    const totalW = n * size.w + (n - 1) * 48
    const s = freeSpot(totalW, size.h)
    x = s.x
    y = s.y
  }

  const created = []
  for (let i = 0; i < n; i++) {
    const node = reactive({
      id: newId('g'), type: 'gen', status: 'running',
      x: Math.round(x + i * (size.w + 48)), y: Math.round(y), w: size.w, h: size.h, z: 0,
      model: model.value?.alias || model.value?.name || model.value?.id || 'AI',
    })
    doc.nodes.push(node)
    for (const s of sources) {
      // 同向已存在就跳过,避免重复生成时边越堆越多
      if (!doc.edges.some((e) => e.from === s.id && e.to === node.id)) {
        doc.edges.push({ id: newId('e'), from: s.id, to: node.id })
      }
    }
    created.push(node)
    runGeneration(node, prompt)
  }
  selectionIds.value = created.map((c) => c.id)
  stage.value?.selectIds(selectionIds.value)
  pushHistory()
}

// 单个生成任务(并发执行,各自更新自己的节点)
async function runGeneration(node, prompt) {
  runningGens.value++
  try {
    const payload = {
      model: gen.modelId, prompt, ratio: gen.ratio || ratios.value[0],
      resolution: gen.resolution || resolutions.value[0] || '1K',
    }
    if (gen.kind === 'video') payload.duration = gen.duration || durations.value[0]
    if (refImages.value.length) {
      payload.reference_images = refImages.value.map((r) => r.dataUrl.replace(/^data:[^,]*,/, ''))
    }
    const opts = jsonBody('POST', payload)
    opts.headers['Idempotency-Key'] = node.id
    const r = await api('/generate', opts)
    if (r.ok && r.data?.url) {
      node.type = gen.kind === 'video' ? 'video' : 'image'
      node.src = r.data.url.replace(/^\/images\//, '')
      node.prompt = prompt
      node.model = model.value?.alias || model.value?.name || model.value?.id
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
    runningGens.value--
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
  } else if (type === 'ref') {
    addNodeAsRef(n)
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
    if (!uploadBusy.value) pickerOpen.value = false
  } else if (e.key === '+' || e.key === '=') {
    stage.value?.zoomStep(1)
  } else if (e.key === '-') {
    stage.value?.zoomStep(-1)
  } else if (e.key === '0') {
    stage.value?.setViewport({ x: 0, y: 0, scale: DEFAULT_SCALE })
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
  const first = kindModels.value[0]
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
  saved: '', dirty: '', saving: '', error: '保存失败,重试中',
}[saveState.value] || ''))
</script>

<template>
  <!-- 传送到 body:布局 <main> 自带 relative z-10 层叠上下文,留在里面会被
       站点固定顶栏(z-40)盖住,画布工具栏无法点击。 -->
  <Teleport to="body">
    <div class="cv-page">
    <!-- 悬浮岛：左上画布切换 / 右上撤销缩放 -->
    <div class="cv-top">
      <div class="cv-island cv-top__left">
        <button class="cv-btn cv-btn--ghost" title="返回" @click="$router.push('/user')"><Icon name="arrow" :size="15" style="transform: rotate(180deg)" /></button>
        <span class="cv-sep" />
        <SelectMenu
          class="cv-dd cv-dd--canvas"
          :model-value="currentId"
          :options="canvasOptions"
          placeholder="选择画布"
          @update:model-value="switchCanvas"
        />
        <button class="cv-btn cv-btn--ghost" title="重命名" @click="renameCanvas">✎</button>
        <button class="cv-btn cv-btn--ghost" title="新建画布" @click="newCanvas"><Icon name="plus" :size="15" /></button>
        <button class="cv-btn cv-btn--ghost cv-btn--danger" title="删除画布" @click="deleteCanvas"><Icon name="trash" :size="15" /></button>
        <span v-if="saveLabel" class="cv-save" :data-state="saveState">{{ saveLabel }}</span>
      </div>

      <div class="cv-island cv-top__right">
        <button class="cv-btn cv-btn--ghost" :disabled="!undoCount" title="撤销 (Ctrl+Z)" @click="undo">↩</button>
        <button class="cv-btn cv-btn--ghost" :disabled="!redoCount" title="重做 (Ctrl+Shift+Z)" @click="redo">↪</button>
        <span class="cv-sep" />
        <button class="cv-btn cv-btn--ghost" title="缩小 10%" @click="stage?.zoomStep(-1)">−</button>
        <span class="cv-zoom">{{ scalePct }}%</span>
        <button class="cv-btn cv-btn--ghost" title="放大 10%" @click="stage?.zoomStep(1)"><Icon name="plus" :size="15" /></button>
        <span class="cv-sep" />
        <button class="cv-btn cv-btn--ghost" title="重置视图 (0)" @click="stage?.setViewport({ x: 0, y: 0, scale: DEFAULT_SCALE })">⌂</button>
        <button class="cv-btn cv-btn--ghost" title="适应内容 (1)" @click="stage?.fitToContent?.()">⤢</button>
      </div>
    </div>

    <!-- 左侧工具坞 -->
    <div class="cv-tools">
      <button class="cv-tool" title="添加笔记" @click="addNote"><Icon name="plus" :size="18" /><span>笔记</span></button>
      <button class="cv-tool" title="从图片库添加" @click="openPicker"><Icon name="image" :size="18" /><span>图片库</span></button>
      <input ref="fileInput" type="file" accept="image/*" multiple hidden @change="onFilePicked">
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

        <!-- 底部居中的生成输入框(参考 vividai.run 的大输入框) -->
        <div class="cv-dock">
          <div class="cv-composer">
            <div v-if="maxRefs > 0" class="cv-composer__refs">
              <div v-for="(r, i) in refImages" :key="i" class="cv-refthumb">
                <img v-if="!r.fileType || r.fileType === 'image'" :src="r.dataUrl" :alt="r.name">
                <span v-else class="cv-refthumb__media">{{ r.fileType === 'video' ? '🎬' : '🔊' }}</span>
                <button type="button" class="cv-refthumb__x" title="移除" @click="removeRef(i)">✕</button>
              </div>
              <button
                v-if="refImages.length < maxRefs"
                type="button"
                class="cv-refadd"
                :title="supportsMediaRefs ? '上传参考图/视频/音频' : '上传参考图'"
                @click="refInput?.click()"
              >
                <span class="cv-refadd__icon">＋</span>
                <span>参考图</span>
                <span class="cv-refadd__n">{{ refImages.length }}/{{ maxRefs }}</span>
              </button>
              <input ref="refInput" type="file" :accept="refFileAccept" multiple hidden @change="onRefPicked">
            </div>
            <textarea
              v-model="gen.prompt"
              class="cv-composer__ta"
              rows="3"
              placeholder="描述你要生成的画面…(留空则使用选中的笔记)"
            />
            <div class="cv-composer__bar">
              <SelectMenu v-model="gen.kind" class="cv-dd" :options="kindOptions" up />
              <SelectMenu
                v-if="kindModels.length"
                v-model="gen.modelId"
                class="cv-dd cv-dd--model"
                :options="modelOptions"
                placeholder="模型"
                up
              />
              <SelectMenu
                v-if="model && ratios.length"
                v-model="gen.ratio"
                class="cv-dd"
                :options="ratioOptions"
                up
              />
              <SelectMenu
                v-if="gen.kind === 'image' && model && resolutions.length"
                v-model="gen.resolution"
                class="cv-dd"
                :options="resolutionOptions"
                up
              />
              <SelectMenu
                v-if="gen.kind === 'video' && model && durations.length"
                v-model="gen.duration"
                class="cv-dd"
                :options="durationOptions"
                up
              />
              <SelectMenu
                v-if="gen.kind === 'image'"
                v-model.number="gen.count"
                class="cv-dd"
                :options="countOptions"
                up
              />
              <div v-if="!kindModels.length" class="cv-nomodels">
                ⚠ 还没有可用的{{ gen.kind === 'video' ? '视频' : '图像' }}模型
              </div>
              <button class="cv-gen" :disabled="genBusy || !model" @click="generate">
                {{ genBusy ? '生成中…' : '生成' }}
              </button>
            </div>
          </div>
          <div v-if="genMsg" class="cv-genmsg">{{ genMsg }}</div>
        </div>
      </div>
    </div>

    <!-- 图片预览(带鉴权 blob,不再依赖 cookie) -->
    <div v-if="previewSrc" class="cv-lightbox" @click="previewSrc = ''">
      <img v-if="previewUrl" :src="previewUrl" alt="" @click.stop>
      <span v-else class="cv-lightbox__loading">加载中…</span>
    </div>

    <!-- 图片库:我的全部作品 + 第一格上传入口 -->
    <div v-if="pickerOpen" class="cv-picker" @click.self="!uploadBusy && (pickerOpen = false)">
      <div class="cv-picker__box">
        <div class="cv-picker__head">
          <span>图片库(点击插入画布)</span>
          <button class="cv-btn" :disabled="uploadBusy" title="上传中,请稍候" @click="pickerOpen = false">✕</button>
        </div>
        <div v-if="pickerLoading" class="cv-loading">加载中…</div>
        <div v-else class="cv-picker__grid">
          <button
            type="button"
            class="cv-picker__up"
            :disabled="uploadBusy"
            title="上传本地图片"
            @click="fileInput?.click()"
          >
            <span class="cv-picker__up-icon">⤒</span>
            <span>{{ uploadBusy ? '上传中…' : '上传图片' }}</span>
          </button>
          <img
            v-for="f in pickerItems"
            :key="f.name"
            :src="pickerUrls[f.name] || undefined"
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
/* ===== 画布:玻璃拟态全屏工作台(随站点明暗主题走变量) ===== */
.cv-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  height: 30px;
  padding: 0 12px;
  border-radius: 10px;
  border: 1px solid var(--ink-soft);
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  color: var(--fg);
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
  cursor: pointer;
  white-space: nowrap;
  box-shadow: var(--glass-shadow);
  transition: transform 0.12s ease, box-shadow 0.12s ease, background 0.12s ease, border-color 0.12s ease;
}
.cv-btn:hover:not(:disabled) {
  background: var(--hover);
  border-color: rgb(24 24 27 / 0.45);
  box-shadow: 0 4px 14px var(--accent-glow);
  transform: translateY(-1px);
}
.cv-btn:active:not(:disabled) {
  transform: translateY(0);
}
.cv-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.cv-btn--danger:hover:not(:disabled) {
  background: #ffb4ab;
  border-color: #e5484d;
  box-shadow: 0 4px 14px rgb(229 72 77 / 0.25);
}
.cv-btn--accent {
  background: var(--surface-solid);
  border-color: rgb(24 24 27 / 0.28);
  color: #18181b;
}
.cv-btn--accent:hover:not(:disabled) {
  background: rgb(24 24 27 / 0.05);
  border-color: #18181b;
  box-shadow: 0 4px 14px rgb(24 24 27 / 0.12);
}
.cv-select,
.cv-input {
  padding: 6px 10px;
  border-radius: 9px;
  border: 1px solid var(--ink-soft);
  background: var(--surface);
  color: var(--fg);
  font-size: 13px;
  outline: none;
}
.cv-select:focus,
.cv-input:focus {
  border-color: rgb(24 24 27 / 0.6);
  box-shadow: 0 0 0 3px rgb(24 24 27 / 0.18);
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
  position: absolute;
  top: 14px;
  left: 14px;
  right: 14px;
  z-index: 20; /* 下拉面板不能被舞台盖住 */
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  pointer-events: none;
}
.cv-island {
  pointer-events: auto;
  padding: 6px;
  border-radius: 16px;
  border: 1px solid rgb(24 24 27 / 0.08);
  background: var(--surface-solid);
  -webkit-backdrop-filter: blur(14px);
  backdrop-filter: blur(14px);
  box-shadow: 0 1px 2px rgb(24 24 27 / 0.04), 0 12px 32px -12px rgb(24 24 27 / 0.2);
}
.cv-btn--ghost {
  border-color: transparent;
  background: transparent;
  box-shadow: none;
  -webkit-backdrop-filter: none;
  backdrop-filter: none;
  width: 32px;
  padding: 0;
  height: 32px;
  border-radius: 10px;
}
.cv-tools {
  position: absolute;
  left: 14px;
  top: 50%;
  transform: translateY(-50%);
  z-index: 15;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px;
  border-radius: 18px;
  border: 1px solid rgb(24 24 27 / 0.08);
  background: var(--surface-solid);
  -webkit-backdrop-filter: blur(14px);
  backdrop-filter: blur(14px);
  box-shadow: 0 1px 2px rgb(24 24 27 / 0.04), 0 12px 32px -12px rgb(24 24 27 / 0.2);
}
.cv-tool {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  width: 56px;
  padding: 9px 0 7px;
  border: 0;
  border-radius: 12px;
  background: transparent;
  color: var(--fg-2);
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.cv-tool:hover { background: rgb(24 24 27 / 0.06); color: #18181b; }
.cv-top__left,
.cv-top__right {
  display: flex;
  align-items: center;
  gap: 2px;
}
/* 统一自定义下拉的宽度/紧凑度(SelectMenu 内部是 .field 按钮) */
.cv-dd {
  min-width: 96px;
}
.cv-dd--canvas {
  width: 200px;
  min-width: 0;
}
.cv-dd :deep(.field) {
  height: 34px;
  padding: 0 10px;
  font-size: 12.5px;
}
.cv-dd--canvas :deep(.field) {
  height: 30px;
}
.cv-dd--zoom {
  display: none;
}
.cv-sep {
  width: 1px;
  height: 18px;
  background: rgb(24 24 27 / 0.12);
  margin: 0 4px;
}
.cv-zoom {
  font-size: 12px;
  font-weight: 700;
  color: var(--fg);
  min-width: 46px;
  text-align: center;
  background: transparent;
  border: 0;
  border-radius: 8px;
  padding: 1px 4px;
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
  border: 2px dashed rgb(24 24 27 / 0.6);
  border-radius: 18px;
  background: rgb(24 24 27 / 0.1);
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

/* 底部居中的生成输入框 —— 悬浮在画布上 */
.cv-dock {
  position: absolute;
  left: 50%;
  bottom: 18px;
  transform: translateX(-50%);
  width: min(760px, calc(100% - 28px));
  z-index: 8;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.cv-ratios {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.cv-chip {
  padding: 3px 11px;
  border-radius: 999px;
  border: 1px solid var(--ink-soft);
  background: var(--surface);
  color: var(--fg);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.13s ease, border-color 0.13s ease, box-shadow 0.13s ease;
}
.cv-chip:hover {
  background: var(--hover);
  border-color: rgb(24 24 27 / 0.4);
}
.cv-chip.is-on {
  color: #fff;
  background: linear-gradient(120deg, #18181b, #27272a);
  border-color: transparent;
  box-shadow: 0 4px 14px var(--accent-glow);
}
/* 生成输入框:参考图缩略图在上,文本框居中,控制条在下 */
.cv-composer {
  width: 100%;
  border: 1px solid rgb(24 24 27 / 0.1);
  border-radius: 22px;
  background: #fff;
  box-shadow: 0 2px 4px rgb(24 24 27 / 0.04), 0 18px 48px -12px rgb(24 24 27 / 0.2);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  overflow: visible; /* 下拉面板要从输入框上方弹出,不能被裁切 */
}
.cv-composer:focus-within {
  border-color: rgb(24 24 27 / 0.55);
  box-shadow: 0 0 0 3px rgb(24 24 27 / 0.08), 0 18px 48px -12px rgb(24 24 27 / 0.24);
}
.cv-composer__refs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 14px 4px;
}
.cv-composer__refs:empty,
.cv-composer__refs:not(:has(> *)) {
  padding-top: 0;
}
.cv-refthumb {
  position: relative;
  width: 56px;
  height: 56px;
  border-radius: 12px;
  overflow: hidden;
  border: 1px solid var(--glass-border);
  background: var(--surface-2);
  box-shadow: var(--glass-shadow);
}
.cv-refthumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.cv-refthumb__media {
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
  font-size: 22px;
}
.cv-refthumb__x {
  position: absolute;
  top: 2px;
  right: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: none;
  display: grid;
  place-items: center;
  font-size: 9px;
  color: #fff;
  background: rgb(24 21 36 / 0.65);
  cursor: pointer;
}
.cv-refthumb__x:hover {
  background: #ef4444;
}
.cv-refadd {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  border: 1.5px dashed rgb(24 24 27 / 0.5);
  background: rgb(24 24 27 / 0.04);
  color: var(--m-orange);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  font-size: 10px;
  font-weight: 700;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease, transform 0.13s ease, box-shadow 0.15s ease;
}
.cv-refadd:hover {
  border-color: var(--m-orange);
  background: rgb(24 24 27 / 0.1);
  transform: translateY(-1px);
  box-shadow: 0 6px 16px var(--accent-glow);
}
.cv-refadd__icon {
  font-size: 15px;
  line-height: 1;
}
.cv-refadd__n {
  font-size: 9px;
  font-weight: 600;
  color: var(--fg-3);
}
.cv-composer__ta {
  width: 100%;
  min-height: 62px;
  padding: 12px 16px;
  border: none;
  background: transparent;
  color: var(--fg);
  font-size: 13.5px;
  line-height: 1.6;
  resize: none;
  outline: none;
  display: block;
}
.cv-composer__ta::placeholder {
  color: var(--fg-faint);
}
.cv-composer__bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 10px 14px;
  border-top: 1px solid var(--ink-soft);
  background: rgb(255 255 255 / 0.02);
  border-radius: 0 0 21px 21px; /* composer 不再裁切,圆角由最底层行补齐 */
}
.cv-composer__bar .cv-dd--model {
  flex: 1;
  min-width: 140px;
  max-width: 260px;
}
.cv-composer__bar .cv-gen {
  margin-left: auto;
  padding: 8px 26px;
  border-radius: 12px;
  letter-spacing: 1px;
}
.cv-gen {
  padding: 10px;
  border-radius: 11px;
  border: 1px solid rgb(255 255 255 / 0.18);
  background: linear-gradient(120deg, #18181b, #27272a 55%, #52525b 140%);
  color: #fff;
  font-size: 14px;
  font-weight: 800;
  cursor: pointer;
  box-shadow: 0 8px 22px var(--accent-glow);
  transition: transform 0.12s ease, box-shadow 0.12s ease, filter 0.12s ease;
}
.cv-gen:hover:not(:disabled) {
  transform: translateY(-1px);
  filter: brightness(1.07);
  box-shadow: 0 12px 30px var(--accent-glow);
}
.cv-gen:active:not(:disabled) {
  transform: translateY(1px) scale(0.99);
  box-shadow: 0 4px 12px var(--accent-glow);
}
.cv-gen:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.cv-genmsg {
  font-size: 11px;
  color: var(--fg-2);
  word-break: break-all;
  text-align: center;
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  border-radius: 999px;
  padding: 3px 14px;
  align-self: center;
  box-shadow: var(--glass-shadow);
}
.cv-nomodels {
  font-size: 11px;
  font-weight: 600;
  color: #92400e;
  line-height: 1.6;
  border: 1px dashed rgb(245 158 11 / 0.6);
  border-radius: 9px;
  background: rgb(245 158 11 / 0.12);
  padding: 8px 10px;
}
.cv-dock .cv-nomodels {
  flex: 1;
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
.cv-lightbox__loading {
  color: rgb(255 255 255 / 0.8);
  font-size: 14px;
  font-weight: 600;
}
.cv-lightbox img {
  max-width: 92vw;
  max-height: 92vh;
  border-radius: 12px;
  border: 1px solid rgb(255 255 255 / 0.5);
  box-shadow: 0 24px 60px rgb(0 0 0 / 0.5), 0 0 40px var(--accent-glow);
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
  background: var(--menu-bg);
  -webkit-backdrop-filter: blur(20px);
  backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--glass-shadow-lg);
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
  border: 1px solid var(--ink-soft);
  cursor: pointer;
  background: var(--surface-2);
  transition: transform 0.12s ease, box-shadow 0.12s ease, border-color 0.12s ease;
}
.cv-picker__grid img:hover {
  transform: translateY(-2px);
  border-color: rgb(24 24 27 / 0.55);
  box-shadow: 0 8px 20px var(--accent-glow);
}
.cv-picker__up {
  width: 100%;
  aspect-ratio: 1;
  border-radius: 10px;
  border: 2px dashed rgb(24 24 27 / 0.45);
  background: transparent;
  color: var(--m-orange);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: transform 0.12s ease, border-color 0.12s ease, background 0.12s ease, box-shadow 0.12s ease;
}
.cv-picker__up:hover {
  transform: translateY(-2px);
  border-color: var(--m-orange);
  background: rgb(24 24 27 / 0.08);
  box-shadow: 0 8px 20px var(--accent-glow);
}
.cv-picker__up:disabled {
  opacity: 0.6;
  cursor: wait;
}
.cv-picker__up-icon {
  font-size: 22px;
  line-height: 1;
}
</style>
