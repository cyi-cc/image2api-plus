<script setup>
// 无限画布舞台:视口变换、指针交互(拖拽/平移/框选/缩放)、视口剔除、连线。
// 文档(nodes/edges)由父级持有;舞台就地修改节点坐标,结构性变更走事件。
//
// 性能:世界层单 transform(GPU 合成);pointermove 全部 rAF 合并;
// visibleNodes 视口剔除只挂载可见节点;连线纯 SVG path 同层渲染。
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  nodesInViewport,
  screenToWorld,
  zoomAt,
  rectsIntersect,
  nodeRect,
} from '../../utils/canvas'
import CanvasNodeView from './CanvasNodeView.vue'

const props = defineProps({
  nodes: { type: Array, required: true },
  edges: { type: Array, required: true },
})
const emit = defineEmits([
  'move-end',        // 拖拽结束(父级 push undo)
  'selection',       // 选中变化 -> ids[]
  'node-action',     // 双击图片等 -> { type:'preview', id }
  'note-commit',     // 笔记提交 -> { id, text }
  'img-size',        // 图片尺寸校正 -> { id, w, h }
  'viewport-rest',   // 视口静止 700ms -> viewport
  'pan-zoom',        // 视口变化(用于百分比显示,高频)
])

const stageEl = ref(null)
const vp = reactive({ x: 0, y: 0, scale: 1 })
const stageSize = reactive({ w: 0, h: 0 })
const selection = ref(new Set())
const marquee = ref(null)
const spaceHeld = ref(false)
const panning = ref(false)

const visibleNodes = computed(() =>
  nodesInViewport(props.nodes, vp, stageSize.w, stageSize.h),
)
const nodeById = computed(() => {
  const m = new Map()
  for (const n of props.nodes) m.set(n.id, n)
  return m
})

const edgePaths = computed(() => {
  const out = []
  for (const e of props.edges) {
    const a = nodeById.value.get(e.from)
    const b = nodeById.value.get(e.to)
    if (!a || !b) continue
    const x1 = a.x + a.w / 2
    const y1 = a.y + a.h / 2
    const x2 = b.x + b.w / 2
    const y2 = b.y + b.h / 2
    const dx = Math.max(60, Math.abs(x2 - x1) * 0.45)
    out.push({
      id: e.id,
      to: e.to,
      d: `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`,
    })
  }
  return out
})

const worldStyle = computed(() => ({
  transform: `translate3d(${vp.x}px, ${vp.y}px, 0) scale(${vp.scale})`,
}))
const gridStyle = computed(() => {
  const cell = Math.max(14, 24 * vp.scale)
  return {
    backgroundSize: `${cell}px ${cell}px`,
    backgroundPosition: `${vp.x}px ${vp.y}px`,
  }
})

// ---- 尺寸/键盘监听 ----
let ro = null
onMounted(() => {
  ro = new ResizeObserver((entries) => {
    const r = entries[0].contentRect
    stageSize.w = r.width
    stageSize.h = r.height
  })
  ro.observe(stageEl.value)
  stageEl.value.addEventListener('wheel', onWheel, { passive: false })
  window.addEventListener('keydown', onSpaceDown)
  window.addEventListener('keyup', onSpaceUp)
  syncZTop()
})
onBeforeUnmount(() => {
  ro?.disconnect()
  stageEl.value?.removeEventListener('wheel', onWheel)
  window.removeEventListener('keydown', onSpaceDown)
  window.removeEventListener('keyup', onSpaceUp)
})

function onSpaceDown(e) {
  if (e.code === 'Space' && !isTypingTarget(e.target) && !e.repeat) {
    spaceHeld.value = true
    e.preventDefault()
  }
}
function onSpaceUp(e) {
  if (e.code === 'Space') spaceHeld.value = false
}
function isTypingTarget(t) {
  return !!(t && t.closest && t.closest('input, textarea, select, [contenteditable="true"]'))
}

// ---- 坐标:一律换算成舞台本地坐标(offsetX 相对事件目标,不可靠) ----
function localXY(e) {
  const r = stageEl.value.getBoundingClientRect()
  return { x: e.clientX - r.left, y: e.clientY - r.top }
}

function hitNode(e) {
  const el = e.target.closest?.('[data-node-id]')
  return el ? nodeById.value.get(el.dataset.nodeId) : null
}

// ---- 指针状态机 ----
let act = null // { mode: 'pan'|'drag'|'marquee', ... }

function onPointerDown(e) {
  // 文本编辑中完全放行
  if (e.target.closest?.('.cv-node--note.is-editing')) return
  const node = hitNode(e)
  const p = localXY(e)

  const wantPan =
    e.button === 1 || e.button === 2 || spaceHeld.value || (!node && !e.shiftKey && e.button === 0)
  if (wantPan) {
    panning.value = true
    act = { mode: 'pan', px: p.x, py: p.y, vx: vp.x, vy: vp.y }
    stageEl.value.setPointerCapture(e.pointerId)
    return
  }
  if (e.button !== 0) return

  if (node) {
    if (e.shiftKey) {
      const s = new Set(selection.value)
      if (s.has(node.id)) s.delete(node.id)
      else s.add(node.id)
      setSelection(s)
    } else if (!selection.value.has(node.id)) {
      setSelection(new Set([node.id]))
    }
    bumpZ(node)
    const origins = [...selection.value]
      .map((id) => nodeById.value.get(id))
      .filter(Boolean)
      .map((n) => ({ n, x: n.x, y: n.y }))
    const start = screenToWorld(p.x, p.y, vp)
    act = { mode: 'drag', start, origins, moved: false, px: p.x, py: p.y }
  } else {
    act = { mode: 'marquee', px: p.x, py: p.y, base: new Set(selection.value) }
    marquee.value = { x: p.x, y: p.y, w: 0, h: 0 }
  }
  stageEl.value.setPointerCapture(e.pointerId)
}

let rafPending = false
let pendingEvent = null
function onPointerMove(e) {
  if (!act) return
  pendingEvent = e
  if (rafPending) return
  rafPending = true
  requestAnimationFrame(() => {
    rafPending = false
    if (pendingEvent) applyMove(pendingEvent)
  })
}

function applyMove(e) {
  if (!act) return
  const p = localXY(e)
  if (act.mode === 'pan') {
    vp.x = act.vx + (p.x - act.px)
    vp.y = act.vy + (p.y - act.py)
    notifyViewport()
    return
  }
  if (act.mode === 'drag') {
    if (!act.moved) {
      if (Math.hypot(p.x - act.px, p.y - act.py) < 3) return
      act.moved = true
    }
    const cur = screenToWorld(p.x, p.y, vp)
    const dx = cur.x - act.start.x
    const dy = cur.y - act.start.y
    for (const o of act.origins) {
      o.n.x = Math.round(o.x + dx)
      o.n.y = Math.round(o.y + dy)
    }
    return
  }
  if (act.mode === 'marquee') {
    marquee.value = {
      x: Math.min(act.px, p.x),
      y: Math.min(act.py, p.y),
      w: Math.abs(p.x - act.px),
      h: Math.abs(p.y - act.py),
    }
    const wr = screenRectToWorld(marquee.value)
    const s = new Set(act.base)
    for (const n of props.nodes) if (rectsIntersect(nodeRect(n), wr)) s.add(n.id)
    setSelection(s)
  }
}

function onPointerUp(e) {
  if (!act) return
  const a = act
  act = null
  marquee.value = null
  panning.value = false
  try { stageEl.value.releasePointerCapture(e.pointerId) } catch { /* 已释放 */ }
  if (a.mode === 'drag' && a.moved) emit('move-end')
}
function onPointerCancel() {
  if (act?.mode === 'drag' && act.moved) emit('move-end')
  act = null
  marquee.value = null
  panning.value = false
}

function onWheel(e) {
  e.preventDefault()
  const p = localXY(e)
  if (e.ctrlKey || e.metaKey) {
    Object.assign(vp, zoomAt(vp, p.x, p.y, vp.scale * Math.exp(-e.deltaY * 0.0018)))
  } else {
    vp.x -= e.deltaX
    vp.y -= e.deltaY
  }
  notifyViewport()
}

// ---- 视口通知(静止 700ms 后通知父级随文档保存) ----
let restTimer = null
function notifyViewport() {
  emit('pan-zoom', { ...vp })
  clearTimeout(restTimer)
  restTimer = setTimeout(() => emit('viewport-rest', { x: vp.x, y: vp.y, scale: vp.scale }), 700)
}

// ---- z 序 ----
let zTop = 1
function syncZTop() {
  for (const n of props.nodes) zTop = Math.max(zTop, n.z || 0)
}
function bumpZ(node) {
  zTop += 1
  node.z = zTop
}

// ---- 选择 ----
function setSelection(s) {
  selection.value = s
  emit('selection', [...s])
}

// ---- 对外 API ----
function setViewport(v) {
  Object.assign(vp, v)
  notifyViewport()
}
function zoomBy(factor) {
  const r = stageEl.value?.getBoundingClientRect()
  const cx = r ? r.width / 2 : 0
  const cy = r ? r.height / 2 : 0
  Object.assign(vp, zoomAt(vp, cx, cy, vp.scale * factor))
  notifyViewport()
}
function centerOn(x, y) {
  const r = stageEl.value?.getBoundingClientRect()
  vp.x = (r ? r.width / 2 : 400) - x * vp.scale
  vp.y = (r ? r.height / 2 : 300) - y * vp.scale
  notifyViewport()
}
function clearSelection() {
  if (selection.value.size) setSelection(new Set())
}
function selectedIds() {
  return [...selection.value]
}
function selectAll() {
  setSelection(new Set(props.nodes.map((n) => n.id)))
}
function screenRectToWorld(r) {
  const a = screenToWorld(r.x, r.y, vp)
  const b = screenToWorld(r.x + r.w, r.y + r.h, vp)
  return {
    x: Math.min(a.x, b.x),
    y: Math.min(a.y, b.y),
    w: Math.abs(b.x - a.x),
    h: Math.abs(b.y - a.y),
  }
}
defineExpose({
  setViewport, zoomBy, centerOn, clearSelection, selectedIds, selectAll, syncZTop, vp,
})
</script>

<template>
  <div
    ref="stageEl"
    class="cv-stage"
    :class="{ 'is-panning': panning || spaceHeld }"
    :style="gridStyle"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointercancel="onPointerCancel"
    @contextmenu.prevent
  >
    <div class="cv-world" :style="worldStyle">
      <svg class="cv-edges" aria-hidden="true">
        <path
          v-for="p in edgePaths"
          :key="p.id"
          :d="p.d"
          class="cv-edge"
          :class="{ 'cv-edge--hot': selection.has(p.to) }"
        />
      </svg>
      <CanvasNodeView
        v-for="n in visibleNodes"
        :key="n.id"
        :node="n"
        :selected="selection.has(n.id)"
        @imgload="(s) => emit('img-size', { id: n.id, ...s })"
        @note-commit="(text) => emit('note-commit', { id: n.id, text })"
        @node-action="(a) => emit('node-action', { ...a, id: n.id })"
      />
    </div>

    <div v-if="marquee" class="cv-marquee" :style="marquee" />
    <div class="cv-hint">拖拽空白平移 · Ctrl+滚轮缩放 · Shift+拖拽框选 · 双击笔记编辑 · 按住空格+拖拽平移</div>
  </div>
</template>

<style scoped>
.cv-stage {
  position: absolute;
  inset: 0;
  overflow: hidden;
  touch-action: none;
  background-color: #0e1015;
  background-image: radial-gradient(circle, rgb(255 255 255 / 0.07) 1px, transparent 1px);
}
.cv-stage.is-panning {
  cursor: grab;
}
.cv-world {
  position: absolute;
  left: 0;
  top: 0;
  width: 0;
  height: 0;
  transform-origin: 0 0;
  will-change: transform;
}
.cv-edges {
  position: absolute;
  left: 0;
  top: 0;
  width: 1px;
  height: 1px;
  overflow: visible;
  pointer-events: none;
}
.cv-edge {
  fill: none;
  stroke: rgb(140 146 165 / 0.5);
  stroke-width: 1.5;
}
.cv-edge--hot {
  stroke: #7c6cff;
  stroke-width: 2;
}
.cv-marquee {
  position: absolute;
  border: 1px solid #7c6cff;
  background: rgb(124 108 255 / 0.12);
  pointer-events: none;
  border-radius: 2px;
}
.cv-hint {
  position: absolute;
  left: 50%;
  bottom: 10px;
  transform: translateX(-50%);
  font-size: 11px;
  color: rgb(255 255 255 / 0.28);
  pointer-events: none;
  white-space: nowrap;
}
</style>
