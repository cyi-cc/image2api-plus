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
  'link-create',     // 手动连线完成 -> { from, to }
  'node-action',     // 双击图片等 -> { type:'preview', id }
  'note-commit',     // 笔记文本提交 -> { id, text }
  'img-size',        // 图片真实尺寸校正 -> { id, w, h }
  'viewport-rest',   // 视口静止 700ms -> viewport
  'pan-zoom',        // 视口变化(用于百分比显示,高频)
  'blank-dblclick',  // 双击空白 -> { x, y } 世界坐标(父级建笔记)
])

const stageEl = ref(null)
const vp = reactive({ x: 0, y: 0, scale: 1 })
const stageSize = reactive({ w: 0, h: 0 })
const selection = ref(new Set())
const edgeSel = ref(new Set())
const marquee = ref(null)
const linkSource = ref(null)
const spaceHeld = ref(false)
const panning = ref(false)

// 视口剔除。尺寸强下限:Teleport/过渡时序下 ResizeObserver 可能给出
// 0/极小/延迟的尺寸,任何一种都不允许把节点剔除出渲染(剔除只是性能
// 优化,正确性优先;多挂载一圈屏幕外节点对千级节点也毫无压力)。
const visibleNodes = computed(() => {
  const w = Math.max(stageSize.w || 0, window.innerWidth || 0, 1600)
  const h = Math.max(stageSize.h || 0, window.innerHeight || 0, 900)
  return nodesInViewport(props.nodes, vp, w, h)
})
const nodeById = computed(() => {
  const m = new Map()
  for (const n of props.nodes) m.set(n.id, n)
  return m
})

function portPoint(n, side) {
  if (side === 'top') return { x: n.x + n.w / 2, y: n.y }
  if (side === 'right') return { x: n.x + n.w, y: n.y + n.h / 2 }
  if (side === 'bottom') return { x: n.x + n.w / 2, y: n.y + n.h }
  if (side === 'left') return { x: n.x, y: n.y + n.h / 2 }
  return { x: n.x + n.w / 2, y: n.y + n.h / 2 }
}
function portDirection(side, fallback) {
  return {
    top: { x: 0, y: -1 }, right: { x: 1, y: 0 },
    bottom: { x: 0, y: 1 }, left: { x: -1, y: 0 },
  }[side] || fallback
}

const EDGE_PAD = 30

function shifted(p, v, d) {
  return { x: p.x + v.x * d, y: p.y + v.y * d }
}
function cubicPoint(p0, c1, c2, p1, t) {
  const u = 1 - t
  return {
    x: u * u * u * p0.x + 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * p1.x,
    y: u * u * u * p0.y + 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * p1.y,
  }
}
function expandedNode(n) {
  return { x: n.x - EDGE_PAD, y: n.y - EDGE_PAD, w: n.w + EDGE_PAD * 2, h: n.h + EDGE_PAD * 2 }
}
function segmentHitsRect(a, b, r) {
  for (let i = 0; i <= 20; i++) {
    const t = i / 20
    const x = a.x + (b.x - a.x) * t
    const y = a.y + (b.y - a.y) * t
    if (x > r.x && x < r.x + r.w && y > r.y && y < r.y + r.h) return true
  }
  return false
}
function polylineHits(points, obstacles) {
  for (let i = 0; i < points.length - 1; i++) {
    for (const r of obstacles) if (segmentHitsRect(points[i], points[i + 1], r)) return true
  }
  return false
}
function cubicHits(p0, c1, c2, p1, obstacles) {
  for (let i = 1; i < 28; i++) {
    const p = cubicPoint(p0, c1, c2, p1, i / 28)
    for (const r of obstacles) {
      if (p.x > r.x && p.x < r.x + r.w && p.y > r.y && p.y < r.y + r.h) return true
    }
  }
  return false
}
function polylineLength(points) {
  let n = 0
  for (let i = 0; i < points.length - 1; i++) n += Math.hypot(points[i + 1].x - points[i].x, points[i + 1].y - points[i].y)
  return n
}
function routePolyline(a, b, obstacles) {
  const mx = (a.x + b.x) / 2
  const my = (a.y + b.y) / 2
  const candidates = [
    [a, b],
    [a, { x: b.x, y: a.y }, b],
    [a, { x: a.x, y: b.y }, b],
    [a, { x: mx, y: a.y }, { x: mx, y: b.y }, b],
    [a, { x: a.x, y: my }, { x: b.x, y: my }, b],
  ]
  if (obstacles.length) {
    const box = obstacles.reduce((o, r) => ({
      x: Math.min(o.x, r.x), y: Math.min(o.y, r.y),
      x2: Math.max(o.x2, r.x + r.w), y2: Math.max(o.y2, r.y + r.h),
    }), { x: Infinity, y: Infinity, x2: -Infinity, y2: -Infinity })
    for (const y of [box.y - EDGE_PAD, box.y2 + EDGE_PAD]) {
      candidates.push([a, { x: a.x, y }, { x: b.x, y }, b])
    }
    for (const x of [box.x - EDGE_PAD, box.x2 + EDGE_PAD]) {
      candidates.push([a, { x, y: a.y }, { x, y: b.y }, b])
    }
  }
  let best = null
  let bestScore = Infinity
  for (const c of candidates) {
    if (polylineHits(c, obstacles)) continue
    const score = polylineLength(c) + (c.length - 2) * 80
    if (score < bestScore) {
      best = c
      bestScore = score
    }
  }
  return best || [a, b]
}
function roundedPath(points) {
  const pts = points.filter((p, i) => !i || Math.hypot(p.x - points[i - 1].x, p.y - points[i - 1].y) > 0.5)
  if (pts.length < 2) return ''
  let d = `M ${pts[0].x} ${pts[0].y}`
  for (let i = 1; i < pts.length - 1; i++) {
    const prev = pts[i - 1]
    const cur = pts[i]
    const next = pts[i + 1]
    const d1 = Math.hypot(cur.x - prev.x, cur.y - prev.y)
    const d2 = Math.hypot(next.x - cur.x, next.y - cur.y)
    const r = Math.min(20, d1 / 2, d2 / 2)
    if (r < 1) continue
    const inX = cur.x - ((cur.x - prev.x) / d1) * r
    const inY = cur.y - ((cur.y - prev.y) / d1) * r
    const outX = cur.x + ((next.x - cur.x) / d2) * r
    const outY = cur.y + ((next.y - cur.y) / d2) * r
    d += ` L ${inX} ${inY} Q ${cur.x} ${cur.y} ${outX} ${outY}`
  }
  const last = pts[pts.length - 1]
  return `${d} L ${last.x} ${last.y}`
}

const edgePaths = computed(() => {
  const out = []
  for (const e of props.edges) {
    const a = nodeById.value.get(e.from)
    const b = nodeById.value.get(e.to)
    if (!a || !b) continue
    const p1 = portPoint(a, e.fromSide)
    const p2 = portPoint(b, e.toSide)
    const v1 = portDirection(e.fromSide, { x: 1, y: 0 })
    const v2 = portDirection(e.toSide, { x: -1, y: 0 })
    const manualBend = Number.isFinite(e.bendX) && Number.isFinite(e.bendY)
      ? { x: (p1.x + p2.x) / 2 + e.bendX, y: (p1.y + p2.y) / 2 + e.bendY }
      : null
    const d = Math.max(60, Math.hypot(p2.x - p1.x, p2.y - p1.y) * 0.45)
    const c1 = shifted(p1, v1, d)
    const c2 = shifted(p2, v2, d)
    let path
    if (manualBend) {
      const L = Math.hypot(p2.x - p1.x, p2.y - p1.y) || 1
      const tx = (p2.x - p1.x) / L
      const ty = (p2.y - p1.y) / L
      const d1 = Math.max(40, Math.hypot(manualBend.x - p1.x, manualBend.y - p1.y) * 0.5)
      const d2 = Math.max(40, Math.hypot(p2.x - manualBend.x, p2.y - manualBend.y) * 0.5)
      path = `M ${p1.x} ${p1.y} C ${p1.x + v1.x * d1} ${p1.y + v1.y * d1}, ${manualBend.x - tx * d1} ${manualBend.y - ty * d1}, ${manualBend.x} ${manualBend.y} `
        + `C ${manualBend.x + tx * d2} ${manualBend.y + ty * d2}, ${p2.x + v2.x * d2} ${p2.y + v2.y * d2}, ${p2.x} ${p2.y}`
    } else {
      path = `M ${p1.x} ${p1.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${p2.x} ${p2.y}`
      const obstacles = props.nodes
        .filter((n) => n.id !== a.id && n.id !== b.id)
        .map(expandedNode)
      if (obstacles.length && cubicHits(p1, c1, c2, p2, obstacles)) {
        const start = shifted(p1, v1, EDGE_PAD)
        const end = shifted(p2, v2, EDGE_PAD)
        path = roundedPath([p1, ...routePolyline(start, end, obstacles), p2])
      }
    }
    out.push({
      id: e.id,
      to: e.to,
      d: path,
      bend: manualBend,
      handle: manualBend || { x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2 },
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

function measure() {
  const r = stageEl.value?.getBoundingClientRect()
  if (r && r.width > 0 && r.height > 0) {
    stageSize.w = r.width
    stageSize.h = r.height
  }
}

function attachStageBits() {
  if (!stageEl.value) return false
  measure()
  if (!ro) {
    ro = new ResizeObserver((entries) => {
      const r = entries[0]?.contentRect
      if (r) {
        stageSize.w = r.width
        stageSize.h = r.height
      }
    })
    ro.observe(stageEl.value)
  }
  stageEl.value.addEventListener('wheel', onWheel, { passive: false })
  window.addEventListener('keydown', onSpaceDown)
  window.addEventListener('keyup', onSpaceUp)
  syncZTop()
  return true
}

let attachTimer = null
onMounted(() => {
  // Teleport + 路由过渡下模板 ref/尺寸偶发延迟:重试到挂上为止
  if (!attachStageBits()) {
    let tries = 0
    clearInterval(attachTimer)
    attachTimer = setInterval(() => {
      tries += 1
      if (attachStageBits() || tries > 20) clearInterval(attachTimer)
    }, 100)
  }
  // rAF 自愈:挂载初期持续测量,直到拿到非零尺寸(过渡动画结束后必命中)
  const heal = () => {
    if (stageSize.w > 0 && stageSize.h > 0) return
    measure()
    requestAnimationFrame(heal)
  }
  requestAnimationFrame(heal)
  // 调试信标(生产可留:只读状态)
  window.__cvDebug = () => ({
    stageSize: { ...stageSize },
    hasEl: !!stageEl.value,
  })
})
onBeforeUnmount(() => {
  clearInterval(attachTimer)
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

  // 0) 缩放手柄:右下角墨边方块,拖动能改节点尺寸
  const sizeGrip = e.target.closest?.('[data-resize]')
  if (sizeGrip && e.button === 0) {
    const node = hitNode(e)
    if (node) {
      clearEdgeSel()
      setSelection(new Set([node.id]))
      const start = screenToWorld(localXY(e).x, localXY(e).y, vp)
      act = {
        mode: 'resize', node,
        startX: start.x, startY: start.y,
        w0: node.w, h0: node.h,
        aspect: node.h > 0 ? node.w / node.h : 1,
      }
      try { stageEl.value.setPointerCapture(e.pointerId) } catch { /* 合成/失效指针 */ }
      return
    }
  }

  // 2) 点住连线:轻点切换选中,拖动则添加/移动弯折点
  const edgeHit = e.target.closest?.('.cv-edge-hit')
  if (edgeHit && e.button === 0) {
    const edge = props.edges.find((x) => x.id === edgeHit.dataset.edgeId)
    if (edge) {
      linkSource.value = null
      const p = localXY(e)
      const w = screenToWorld(p.x, p.y, vp)
      act = {
        mode: 'edge', edge,
        id: edge.id,
        wasSelected: edgeSel.value.has(edge.id),
        start: w,
        bendX: Number.isFinite(edge.bendX) ? edge.bendX : 0,
        bendY: Number.isFinite(edge.bendY) ? edge.bendY : 0,
        moved: false,
        px: p.x, py: p.y,
      }
      try { stageEl.value.setPointerCapture(e.pointerId) } catch { /* 合成/失效指针 */ }
    }
    return
  }

  const node = hitNode(e)
  if (!node || node.id === linkSource.value?.id) linkSource.value = null
  const p = localXY(e)

  const wantPan =
    e.button === 1 || e.button === 2 || spaceHeld.value || (!node && !e.shiftKey && e.button === 0)
  if (wantPan) {
    panning.value = true
    act = { mode: 'pan', px: p.x, py: p.y, vx: vp.x, vy: vp.y }
    try { stageEl.value.setPointerCapture(e.pointerId) } catch { /* 合成/失效指针 */ }
    return
  }
  if (e.button !== 0) return

  if (node) {
    clearEdgeSel()
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
    // 不立即 setPointerCapture:捕获会把后续 click/dblclick 的目标重定向到
    // 舞台,双击编辑等点击语义就全丢了。拖过阈值后再捕获。
    act = { mode: 'drag', start, origins, moved: false, px: p.x, py: p.y, pid: e.pointerId }
  } else {
    act = { mode: 'marquee', px: p.x, py: p.y, base: new Set(selection.value) }
    marquee.value = { x: p.x, y: p.y, w: 0, h: 0 }
  }
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
      try { stageEl.value.setPointerCapture(act.pid) } catch { /* 已释放 */ }
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
  if (act.mode === 'edge') {
    if (!act.moved) {
      if (Math.hypot(p.x - act.px, p.y - act.py) < 4) return
      act.moved = true
      edgeSel.value = new Set([act.id])
      clearSelection()
    }
    const cur = screenToWorld(p.x, p.y, vp)
    const a = nodeById.value.get(act.edge.from)
    const b = nodeById.value.get(act.edge.to)
    if (!a || !b) return
    const p1 = portPoint(a, act.edge.fromSide)
    const p2 = portPoint(b, act.edge.toSide)
    act.edge.bendX = Math.round(cur.x - (p1.x + p2.x) / 2)
    act.edge.bendY = Math.round(cur.y - (p1.y + p2.y) / 2)
    return
  }
  if (act.mode === 'resize') {
    const cur = screenToWorld(p.x, p.y, vp)
    const n = act.node
    if (n.type === 'note') {
      n.w = Math.max(140, Math.round(act.w0 + cur.x - act.startX))
      n.h = Math.max(90, Math.round(act.h0 + cur.y - act.startY))
    } else {
      // 图片锁长宽比:以宽度为准推导高度
      const w = Math.max(96, act.w0 + cur.x - act.startX)
      n.w = Math.round(w)
      n.h = Math.round(w / act.aspect)
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
  if (a.mode === 'resize') emit('move-end') // 尺寸进了快照,可撤销
  if (a.mode === 'edge') {
    if (a.moved) {
      emit('move-end')
    } else {
      const s = new Set(edgeSel.value)
      if (a.wasSelected) s.delete(a.id)
      else s.add(a.id)
      edgeSel.value = s
      if (s.size) clearSelection()
    }
  }
}
function onPointerCancel() {
  if (act?.mode === 'drag' && act.moved) emit('move-end')
  if (act?.mode === 'resize') emit('move-end')
  if (act?.mode === 'edge' && act.moved) emit('move-end')
  act = null
  marquee.value = null
  panning.value = false
}

function onPortClick(id, side) {
  if (linkSource.value && !nodeById.value.has(linkSource.value.id)) linkSource.value = null
  if (!linkSource.value) {
    clearEdgeSel()
    linkSource.value = { id, side }
    return
  }
  if (linkSource.value.id === id) {
    linkSource.value = null
    return
  }
  emit('link-create', {
    from: linkSource.value.id, to: id,
    fromSide: linkSource.value.side, toSide: side,
  })
  linkSource.value = null
}

// 取消进行中的连线(Esc)
function cancelLink() {
  linkSource.value = null
}

// 返回并清空选中的连线(父级 Delete 处理用)
function consumeSelectedEdges() {
  const ids = [...edgeSel.value]
  edgeSel.value = new Set()
  return ids
}
function clearEdgeSel() {
  if (edgeSel.value.size) edgeSel.value = new Set()
}
function hasSelectedEdges() {
  return edgeSel.value.size > 0
}

function nextZoom(scale, direction) {
  if (direction < 0 && scale < 0.1) return scale
  const level = scale * 10
  const step = direction > 0 ? Math.floor(level + 1e-6) + 1 : Math.ceil(level - 1e-6) - 1
  return Math.min(4, Math.max(0.1, step / 10))
}
let wheelAccum = 0
let lastWheelAt = 0
function onWheel(e) {
  e.preventDefault()
  const p = localXY(e)
  if (e.ctrlKey || e.metaKey) {
    const now = Date.now()
    if (now - lastWheelAt > 200) wheelAccum = 0
    lastWheelAt = now
    const delta = e.deltaY * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? 400 : 1)
    let steps
    if (Math.abs(delta) >= 80) {
      steps = Math.sign(delta) * Math.max(1, Math.round(Math.abs(delta) / 120))
      wheelAccum = 0
    } else {
      wheelAccum += delta
      steps = Math.trunc(wheelAccum / 80)
      wheelAccum -= steps * 80
    }
    let scale = vp.scale
    for (let i = 0; i < Math.min(Math.abs(steps), 20); i++) scale = nextZoom(scale, -steps)
    if (scale !== vp.scale) Object.assign(vp, zoomAt(vp, p.x, p.y, scale))
    else return
  } else {
    wheelAccum = 0
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
// 父级复制/成组操作后同步舞台内部选择集
function selectIds(ids) {
  setSelection(new Set(ids))
}
function bumpNode(id) {
  const n = nodeById.value.get(id)
  if (n) bumpZ(n)
}

// ---- 对外 API ----
function setViewport(v) {
  Object.assign(vp, v)
  notifyViewport()
}
function zoomStep(direction) {
  const r = stageEl.value?.getBoundingClientRect()
  const cx = r ? r.width / 2 : 0
  const cy = r ? r.height / 2 : 0
  const scale = nextZoom(vp.scale, direction)
  if (scale === vp.scale) return
  Object.assign(vp, zoomAt(vp, cx, cy, scale))
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
// 双击空白:交给父级在落点建笔记
function onDblClick(e) {
  if (e.target.closest?.('[data-node-id]') || e.target.closest?.('.cv-edge-hit')) return
  const p = localXY(e)
  const w = screenToWorld(p.x, p.y, vp)
  emit('blank-dblclick', { x: w.x, y: w.y })
}
// 适应内容:把所有节点缩放进视口(留 80px 边距,最大放到 1.2x)
function fitToContent() {
  const r = stageEl.value?.getBoundingClientRect()
  const sw = r ? r.width : 800
  const sh = r ? r.height : 600
  const nodes = props.nodes
  if (!nodes.length) {
    setViewport({ x: 0, y: 0, scale: 1 })
    return
  }
  let x1 = Infinity, y1 = Infinity, x2 = -Infinity, y2 = -Infinity
  for (const n of nodes) {
    x1 = Math.min(x1, n.x); y1 = Math.min(y1, n.y)
    x2 = Math.max(x2, n.x + n.w); y2 = Math.max(y2, n.y + n.h)
  }
  const pad = 80
  const bw = Math.max(1, x2 - x1 + pad * 2)
  const bh = Math.max(1, y2 - y1 + pad * 2)
  const s = Math.min(sw / bw, sh / bh, 1.2)
  vp.scale = s
  vp.x = sw / 2 - (x1 - pad + bw / 2) * s
  vp.y = sh / 2 - (y1 - pad + bh / 2) * s
  notifyViewport()
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
  setViewport, zoomStep, centerOn, clearSelection, selectedIds, selectAll, syncZTop, vp,
  cancelLink, consumeSelectedEdges, hasSelectedEdges, fitToContent, selectIds, bumpNode,
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
    @dblclick="onDblClick"
    @contextmenu.prevent
  >
    <div class="cv-world" :style="worldStyle">
      <svg class="cv-edges" aria-hidden="true">
        <g v-for="p in edgePaths" :key="p.id">
          <path
            class="cv-edge-hit"
            :data-edge-id="p.id"
            :d="p.d"
          />
          <path
            class="cv-edge"
            :class="{ 'cv-edge--hot': selection.has(p.to), 'cv-edge--sel': edgeSel.has(p.id) }"
            :d="p.d"
          />
          <circle
            v-if="edgeSel.has(p.id)"
            class="cv-edge-bend"
            :cx="p.handle.x"
            :cy="p.handle.y"
            r="8"
          />
        </g>
      </svg>
      <CanvasNodeView
        v-for="n in visibleNodes"
        :key="n.id"
        :node="n"
        :selected="selection.has(n.id)"
        :link-source-side="linkSource?.id === n.id ? linkSource.side : ''"
        @port-click="(side) => onPortClick(n.id, side)"
        @imgload="(s) => emit('img-size', { id: n.id, ...s })"
        @note-commit="(text) => emit('note-commit', { id: n.id, text })"
        @node-action="(a) => emit('node-action', { ...a, id: n.id })"
      />
    </div>

    <div v-if="marquee" class="cv-marquee" :style="marquee" />
    <div class="cv-hint">拖拽空白平移 · Ctrl+滚轮缩放 · 依次点击连接点连线（拖线调弧度） · 双击笔记编辑</div>
  </div>
</template>

<style scoped>
.cv-stage {
  position: absolute;
  inset: 0;
  overflow: hidden;
  touch-action: none;
  background-color: var(--app-bg);
  background-image: radial-gradient(
    circle,
    color-mix(in srgb, var(--fg) 16%, transparent) 1.4px,
    transparent 1.4px
  );
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
}
.cv-edge {
  fill: none;
  stroke: var(--m-orange-2);
  stroke-width: 2.4;
  filter: drop-shadow(0 0 5px rgb(34 211 238 / 0.35));
}
.cv-edge--hot {
  stroke: var(--m-orange);
  stroke-width: 2.8;
  filter: drop-shadow(0 0 7px var(--accent-glow));
}
.cv-edge--sel {
  stroke: var(--m-orange);
  stroke-width: 3.5;
  stroke-dasharray: 10 6;
  filter: drop-shadow(0 0 8px var(--accent-glow));
}
.cv-edge-hit {
  fill: none;
  stroke: transparent;
  stroke-width: 18;
  pointer-events: stroke;
  cursor: grab;
}
.cv-edge-hit:active {
  cursor: grabbing;
}
.cv-edge-bend {
  fill: var(--surface-solid);
  stroke: var(--m-orange);
  stroke-width: 3;
  pointer-events: none;
  filter: drop-shadow(0 0 6px var(--accent-glow));
}
.cv-marquee {
  position: absolute;
  border: 2px dashed rgb(124 92 252 / 0.5);
  background: rgb(124 92 252 / 0.08);
  pointer-events: none;
  border-radius: 4px;
}
.cv-hint {
  position: absolute;
  left: 50%;
  bottom: 10px;
  transform: translateX(-50%);
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  border: 1px solid var(--glass-border);
  border-radius: 999px;
  padding: 3px 14px;
  box-shadow: var(--glass-shadow);
  pointer-events: none;
  white-space: nowrap;
  max-width: 92%;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
