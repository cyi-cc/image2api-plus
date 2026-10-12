<script setup>
// 单个画布节点。位置/尺寸由 stage 的世界层 transform 统一缩放,
// 这里只负责节点自身内容:image / note / gen(生成中占位)。
// 性能:contain 隔离布局;图片 lazy + async 解码 + 缩略图。
import { computed, ref, onMounted, watch } from 'vue'
import { generatedUrl, thumbUrl } from '../../api'

const props = defineProps({
  node: { type: Object, required: true },
  selected: { type: Boolean, default: false },
  linkSourceSide: { type: String, default: '' },
})
const emit = defineEmits(['imgload', 'note-commit', 'node-action', 'port-click'])
const portSides = ['top', 'right', 'bottom', 'left']
const portLabels = { top: '上', right: '右', bottom: '下', left: '左' }

const imgLoaded = ref(false)
const imgBroken = ref(false)
const imgStatus = ref('')
const imgThumb = computed(() => thumbUrl(props.node.src))
const imgOriginal = computed(() => generatedUrl(props.node.src))
const imgOriginalAbs = computed(() => {
  try { return new URL(imgOriginal.value, window.location.href).href } catch { return imgOriginal.value }
})
const noteEl = ref(null)
const editing = ref(false)

onMounted(() => {
  // 非受控 contenteditable:只在挂载时写初值,避免输入时光标被 v-model 回写打跳。
  if (props.node.type === 'note' && noteEl.value) noteEl.value.innerText = props.node.text || ''
})

// 撤销/重做会整体替换 nodes 数组但复用组件(key 不变),DOM 文字会变成
// 旧值 —— 非编辑状态下把 node.text 同步回 DOM。
watch(
  () => props.node.text,
  (t) => {
    if (props.node.type !== 'note' || editing.value || !noteEl.value) return
    const want = t || ''
    if (noteEl.value.innerText !== want) noteEl.value.innerText = want
  },
)

watch(
  () => props.node.src,
  () => {
    imgLoaded.value = false
    imgBroken.value = false
  },
)

function onImgLoad(e) {
  imgLoaded.value = true
  imgBroken.value = false
  const img = e.target
  // 按真实长宽比校正节点尺寸(以长边 512 归一,保持画布视觉密度一致)。
  if (img.naturalWidth && img.naturalHeight) {
    const long = Math.max(img.naturalWidth, img.naturalHeight)
    const k = 512 / long
    emit('imgload', {
      w: Math.round(img.naturalWidth * k),
      h: Math.round(img.naturalHeight * k),
    })
  }
}

function onImgError(e) {
  if (e.target.src !== imgOriginalAbs.value) {
    e.target.src = imgOriginal.value
    return
  }
  imgLoaded.value = true
  imgBroken.value = true
  imgStatus.value = ''
  fetch(imgOriginal.value, { credentials: 'include' })
    .then((r) => { imgStatus.value = String(r.status) })
    .catch(() => { imgStatus.value = '网络错误' })
}

function startEdit() {
  if (props.node.type !== 'note') return
  editing.value = true
  requestAnimationFrame(() => {
    const el = noteEl.value
    if (el) {
      el.innerText = props.node.text || ''
      el.focus()
      // 光标落末尾
      const sel = window.getSelection()
      const range = document.createRange()
      range.selectNodeContents(el)
      range.collapse(false)
      sel.removeAllRanges()
      sel.addRange(range)
    }
  })
}

function commitNote() {
  const el = noteEl.value
  editing.value = false
  if (!el) return
  // 不依赖 editing 标志做早退:焦点/事件时序可能把标志先置 false,
  // 但用户输入的文字仍然必须被提交(否则静默丢字)。
  const text = (el.innerText || '').replace(/\u00a0/g, ' ').slice(0, 4000)
  if (text !== (props.node.text || '')) emit('note-commit', text)
}

// 双击:笔记 → 进入编辑(自己处理);图片 → 交给父级开预览。
function onSelfDblClick(e) {
  if (props.node.type === 'note') {
    e.stopPropagation()
    startEdit()
  } else if (props.node.type === 'image') {
    e.stopPropagation()
    emit('node-action', { type: 'preview' })
  }
}

defineExpose({ startEdit, commitNote })
</script>

<template>
  <div
    class="cv-node"
    :class="[
      `cv-node--${node.type}`,
      { 'is-selected': selected, 'is-editing': editing, 'is-failed': node.type === 'gen' && node.status === 'failed' },
    ]"
    :style="{ left: node.x + 'px', top: node.y + 'px', width: node.w + 'px', height: node.h + 'px', zIndex: node.z || 0 }"
    :data-node-id="node.id"
    @dblclick="onSelfDblClick"
  >
    <!-- 连线出口:点选起点和终点建立连线 -->
    <button v-for="side in portSides" :key="side"
            type="button" class="cv-port" :class="[`cv-port--${side}`, { 'is-active': linkSourceSide === side }]"
            :aria-label="`${portLabels[side]}侧连接点`" :aria-pressed="linkSourceSide === side"
            :title="`${portLabels[side]}侧连接点`"
            @pointerdown.stop @dblclick.stop @click.stop="emit('port-click', side)" />
    <!-- 右下角缩放手柄 + 悬浮工具条:仅选中时显示,舞台接管 resize 手势 -->
    <span v-if="selected && node.type !== 'gen'" class="cv-size" data-resize />
    <div v-if="selected && node.type !== 'gen'" class="cv-tools" @dblclick.stop @pointerdown.stop>
      <button v-if="node.type === 'image'" title="预览" @click.stop="emit('node-action', { type: 'preview' })">⤢</button>
      <button title="复制 (Ctrl+D)" @click.stop="emit('node-action', { type: 'duplicate' })">⧉</button>
      <button title="置顶" @click.stop="emit('node-action', { type: 'front' })">⤒</button>
      <button class="cv-tools__del" title="删除" @click.stop="emit('node-action', { type: 'delete' })">✕</button>
    </div>
    <div class="cv-node__clip">
    <!-- 图片节点 -->
    <template v-if="node.type === 'image'">
      <img
        v-if="!imgBroken"
        class="cv-node__img"
        :src="imgThumb"
        loading="lazy"
        decoding="async"
        draggable="false"
        alt=""
        @load="onImgLoad"
        @error="onImgError"
      >
      <div v-else class="cv-node__missing">图片不可用<small v-if="imgStatus">HTTP {{ imgStatus }}</small></div>
      <div v-if="node.prompt" class="cv-node__cap" :title="node.prompt">{{ node.prompt }}</div>
    </template>

    <!-- 笔记节点 -->
    <div
      v-else-if="node.type === 'note'"
      ref="noteEl"
      class="cv-node__note"
      :contenteditable="editing"
      spellcheck="false"
      @blur="commitNote"
      @keydown.enter.exact.prevent="commitNote"
    />

    <!-- 生成占位节点 -->
    <div v-else class="cv-node__gen">
      <template v-if="node.status === 'running'">
        <span class="cv-node__spin" />
        <span>生成中…</span>
      </template>
      <template v-else>
        <span class="cv-node__x">✕</span>
        <span>生成失败</span>
        <span class="cv-node__err" :title="node.error || ''">{{ node.error || '请检查提示词后重试' }}</span>
      </template>
    </div>
    </div>
  </div>
</template>

<style scoped>
.cv-node {
  position: absolute;
  border-radius: 14px;
  border: 1px solid var(--glass-border);
  contain: layout style;
  user-select: none;
  touch-action: none;
  cursor: grab;
  background: var(--surface-solid);
  box-shadow: var(--glass-shadow);
  transition: box-shadow 0.15s ease, border-color 0.15s ease;
}
.cv-node__clip {
  position: absolute;
  inset: 0;
  border-radius: 13px;
  overflow: hidden;
}
.cv-node.is-selected {
  border-color: rgb(124 92 252 / 0.7);
  box-shadow: var(--glass-shadow-lg), 0 0 0 3px rgb(124 92 252 / 0.25), 0 10px 30px var(--accent-glow);
}
.cv-node:active {
  cursor: grabbing;
}

.cv-node__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  background: var(--surface-2);
}
.cv-node__missing small { display: block; margin-top: 4px; font-weight: 500; opacity: 0.7; }
.cv-node__missing {
  width: 100%;
  height: 100%;
  align-content: center;
  text-align: center;
  display: grid;
  place-items: center;
  color: var(--fg-3);
  font-size: 13px;
  font-weight: 600;
  background: var(--surface-2);
}

.cv-node__cap {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  padding: 5px 9px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.35;
  color: var(--fg);
  background: var(--surface-solid);
  border-top: 1px solid var(--ink-soft);
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  pointer-events: none;
}

/* 笔记 = 淡黄色贴纸 */
.cv-node--note {
  background: #fdf6c9;
  border-color: rgb(180 140 20 / 0.35);
  cursor: text;
}
html.dark .cv-node--note {
  background: rgb(250 204 21 / 0.14);
  border-color: rgb(250 204 21 / 0.35);
}
.cv-node--note:not(.is-editing) {
  cursor: grab;
}
.cv-node__note {
  width: 100%;
  height: 100%;
  padding: 10px 12px;
  font-size: 13px;
  line-height: 1.55;
  color: #1a1a1a;
  outline: none;
  white-space: pre-wrap;
  word-break: break-word;
  overflow: hidden;
  user-select: none;
}
html.dark .cv-node__note { color: rgb(255 255 255 / 0.88); }
.is-editing .cv-node__note {
  user-select: text;
  cursor: text;
}

.cv-node--gen {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  background: var(--surface);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  border-style: dashed;
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 600;
}
.cv-node--gen.is-failed {
  border-color: #e5484d;
  color: #e5484d;
  background: #ffe4e6;
}
.cv-node__spin {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 3px solid var(--ink-soft);
  border-top-color: var(--m-orange);
  animation: cv-spin 0.9s linear infinite;
}
@keyframes cv-spin {
  to { transform: rotate(360deg); }
}
.cv-node__err {
  font-size: 11px;
  line-height: 1.5;
  max-width: min(90%, 320px);
  max-height: 5em;
  overflow: auto;
  overflow-wrap: anywhere;
  text-align: center;
  user-select: text;
}

.cv-port {
  position: absolute;
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  background: transparent;
  cursor: pointer;
  z-index: 3;
  touch-action: manipulation;
}
.cv-port::after {
  content: '';
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  border: 3px solid var(--m-orange);
  box-shadow: 0 1px 4px rgb(0 0 0 / 0.35), 0 0 0 2px rgb(255 255 255 / 0.8);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.cv-port:hover::after, .cv-port:focus-visible::after, .cv-port.is-active::after {
  transform: scale(1.3);
  box-shadow: 0 0 0 4px rgb(124 92 252 / 0.25), 0 2px 10px var(--accent-glow);
}
.cv-port.is-active::after { background: var(--m-orange); }
.cv-port:focus-visible { outline: 2px solid var(--m-orange); outline-offset: -2px; border-radius: 50%; }
.cv-port--top { top: -17px; left: 50%; transform: translateX(-50%); }
.cv-port--right { right: -17px; top: 50%; transform: translateY(-50%); }
.cv-port--bottom { bottom: -17px; left: 50%; transform: translateX(-50%); }
.cv-port--left { left: -17px; top: 50%; transform: translateY(-50%); }

/* 右下角缩放方块(节点 contain:paint,只能放内侧) */
.cv-size {
  position: absolute;
  right: 4px;
  bottom: 4px;
  width: 15px;
  height: 15px;
  border-radius: 4px;
  background: var(--surface-solid);
  border: 1px solid var(--ink);
  cursor: nwse-resize;
  z-index: 3;
}

/* 选中悬浮工具条:贴节点内上缘(节点 overflow:hidden,外侧会被裁) */
.cv-tools {
  position: absolute;
  top: 6px;
  right: 6px;
  display: flex;
  gap: 4px;
  z-index: 4;
}
.cv-tools button {
  width: 24px;
  height: 24px;
  border-radius: 7px;
  border: 1px solid var(--glass-border);
  background: var(--menu-bg);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  color: var(--fg);
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  box-shadow: var(--glass-shadow);
  display: grid;
  place-items: center;
  padding: 0;
  transition: transform 0.12s ease, background 0.12s ease, color 0.12s ease;
}
.cv-tools button:hover {
  transform: translateY(-1px);
  background: var(--hover);
}
.cv-tools__del {
  color: #e5484d;
}
.cv-tools__del:hover {
  background: rgb(229 72 77 / 0.14) !important;
}
</style>
