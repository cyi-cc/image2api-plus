// 无限画布 — 纯函数层:坐标变换、几何、文档校验。
// 约定:world 坐标(浮点 px)经 viewport 变换到屏幕:
//   screen = world * scale + t   (t = viewport.x/y,即世界原点的屏幕位置)
// 整个世界层只做一个 CSS transform,GPU 合成,平移缩放零重排。

let idSeq = 0

/** 世界→屏幕 */
export function worldToScreen(x, y, vp) {
  return { x: x * vp.scale + vp.x, y: y * vp.scale + vp.y }
}

/** 屏幕→世界 */
export function screenToWorld(sx, sy, vp) {
  return { x: (sx - vp.x) / vp.scale, y: (sy - vp.y) / vp.scale }
}

export const MIN_SCALE = 0.05
export const MAX_SCALE = 4

/** 以屏幕点 m 为锚缩放到 newScale,返回新 viewport(平移量按锚点保持不动) */
export function zoomAt(vp, mx, my, newScale) {
  const s = Math.min(MAX_SCALE, Math.max(MIN_SCALE, newScale))
  const k = s / vp.scale
  return { scale: s, x: mx - (mx - vp.x) * k, y: my - (my - vp.y) * k }
}

/** 节点世界包围盒 */
export function nodeRect(n) {
  return { x: n.x, y: n.y, w: n.w, h: n.h }
}

export function rectsIntersect(a, b) {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

export function pointInRect(px, py, r) {
  return px >= r.x && px <= r.x + r.w && py >= r.y && py <= r.y + r.h
}

/** 视口剔除:返回与可见世界矩形(含 margin)相交的节点。纯几何,千级节点也是微秒。 */
export function nodesInViewport(nodes, vp, stageW, stageH, margin = 240) {
  const vis = {
    x: -vp.x / vp.scale - margin,
    y: -vp.y / vp.scale - margin,
    w: stageW / vp.scale + margin * 2,
    h: stageH / vp.scale + margin * 2,
  }
  return nodes.filter((n) => rectsIntersect(nodeRect(n), vis))
}

/** 常用比例 → 世界像素尺寸(长边 512;图片 onload 后会按真实尺寸校正) */
export function ratioToSize(ratio) {
  const map = {
    '1:1': [512, 512],
    '16:9': [896, 504],
    '9:16': [504, 896],
    '4:3': [592, 444],
    '3:4': [444, 592],
    '3:2': [624, 416],
    '2:3': [416, 624],
    '21:9': [960, 412],
  }
  const [w, h] = map[ratio] || [512, 512]
  return { w, h }
}

export function newId(prefix) {
  idSeq = (idSeq + 1) % 100000
  return `${prefix}_${Date.now().toString(36)}${idSeq.toString(36)}${Math.random()
    .toString(36)
    .slice(2, 7)}`
}

/**
 * 校验并规范化从后端来的文档。宁可丢弃坏节点也不让一个坏字段搞挂整张画布。
 * 返回 { viewport, nodes, edges }。
 */
export function normalizeDoc(raw) {
  const doc = {
    viewport: { x: 0, y: 0, scale: 1 },
    nodes: [],
    edges: [],
  }
  if (!raw || typeof raw !== 'object') return doc
  const vp = raw.viewport
  if (vp && Number.isFinite(vp.x) && Number.isFinite(vp.y) && Number.isFinite(vp.scale)) {
    doc.viewport = {
      x: vp.x,
      y: vp.y,
      scale: Math.min(MAX_SCALE, Math.max(MIN_SCALE, vp.scale)),
    }
  }
  const ids = new Set()
  if (Array.isArray(raw.nodes)) {
    for (const n of raw.nodes) {
      if (!n || typeof n.id !== 'string' || ids.has(n.id)) continue
      if (!Number.isFinite(n.x) || !Number.isFinite(n.y)) continue
      if (!Number.isFinite(n.w) || n.w <= 0 || !Number.isFinite(n.h) || n.h <= 0) continue
      const type = ['note', 'gen', 'video'].includes(n.type) ? n.type : 'image'
      const node = {
        id: n.id,
        type,
        x: n.x,
        y: n.y,
        w: Math.min(4096, n.w),
        h: Math.min(4096, n.h),
        z: Number.isFinite(n.z) ? n.z : 0,
      }
      if (type === 'note') node.text = typeof n.text === 'string' ? n.text.slice(0, 4000) : ''
      if (type === 'image' || type === 'video') {
        node.src = typeof n.src === 'string' ? n.src.slice(0, 500) : ''
        if (!node.src) continue // 媒体节点必须有 src
        if (typeof n.prompt === 'string') node.prompt = n.prompt.slice(0, 2000)
        if (typeof n.model === 'string') node.model = n.model.slice(0, 120)
      }
      if (type === 'gen') {
        node.status = n.status === 'failed' ? 'failed' : 'running'
        if (typeof n.error === 'string') node.error = n.error.slice(0, 500)
      }
      ids.add(node.id)
      doc.nodes.push(node)
    }
  }
  if (Array.isArray(raw.edges)) {
    const pairs = new Set() // 同一对节点只保留一条线(旧文档可能有重复)
    for (const e of raw.edges) {
      if (!e || typeof e.id !== 'string') continue
      if (typeof e.from !== 'string' || typeof e.to !== 'string') continue
      if (!ids.has(e.from) || !ids.has(e.to) || e.from === e.to) continue
      const pair = e.from < e.to ? `${e.from}|${e.to}` : `${e.to}|${e.from}`
      if (pairs.has(pair)) continue
      pairs.add(pair)
      const edge = { id: e.id, from: e.from, to: e.to }
      if (['top', 'right', 'bottom', 'left'].includes(e.fromSide)) edge.fromSide = e.fromSide
      if (['top', 'right', 'bottom', 'left'].includes(e.toSide)) edge.toSide = e.toSide
      if (Number.isFinite(e.bendX) && Number.isFinite(e.bendY)) {
        edge.bendX = Math.max(-10000, Math.min(10000, e.bendX))
        edge.bendY = Math.max(-10000, Math.min(10000, e.bendY))
      }
      doc.edges.push(edge)
    }
  }
  return doc
}

/** 序列化(仅内容,不含 viewport 的可变性;viewport 单独存) */
export function serializeDoc(doc) {
  return JSON.stringify({
    version: 1,
    viewport: doc.viewport,
    nodes: doc.nodes,
    edges: doc.edges,
  })
}

/** 生成节点完成 → 转为图片节点(保留位置/尺寸/边) */
export function promoteGenNode(node, src, extras = {}) {
  node.type = 'image'
  node.src = src
  delete node.status
  delete node.error
  Object.assign(node, extras)
}
