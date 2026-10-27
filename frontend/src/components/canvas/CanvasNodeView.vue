<script setup>
// 单个画布节点。位置/尺寸由 stage 的世界层 transform 统一缩放,
// 这里只负责节点自身内容:image / note / gen(生成中占位)。
// 性能:contain 隔离布局;图片 lazy + async 解码 + 缩略图。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { generatedUrl, loadImageBlob, thumbUrl } from '../../api'

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
const imgBlob = ref('')
const imgOriginal = computed(() => generatedUrl(props.node.src))
const noteEl = ref(null)
const editing = ref(false)

let imgController = null
async function loadSrc() {
  if (!['image', 'video'].includes(props.node.type) || !props.node.src) return
  imgController?.abort()
  imgController = new AbortController()
  imgLoaded.value = false
  imgBroken.value = false
  if (imgBlob.value) { URL.revokeObjectURL(imgBlob.value); imgBlob.value = '' }
  try {
    const { url } = await loadImageBlob(props.node.src, imgController.signal)
    imgBlob.value = url
  } catch (e) {
    imgBroken.value = true
    const msg = e?.message || ''
    const m = msg.match(/\((\d+)\)/)
    imgStatus.value = m ? m[1] : '失败'
    imgLoaded.value = true
  }
}

onMounted(() => loadSrc())
onUnmounted(() => {
  imgController?.abort()
  if (imgBlob.value) { URL.revokeObjectURL(imgBlob.value); imgBlob.value = '' }
})
watch(() => props.node.src, loadSrc)

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
function onVidMeta(e) {
  imgLoaded.value = true
  imgBroken.value = false
  const v = e.target
  if (v.videoWidth && v.videoHeight) {
    const long = Math.max(v.videoWidth, v.videoHeight)
    const k = 512 / long
    emit('imgload', {
      w: Math.round(v.videoWidth * k),
      h: Math.round(v.videoHeight * k),
    })
  }
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
    <div v-if="node.type !== 'gen'" class="cv-tools" @dblclick.stop @pointerdown.stop>
      <button v-if="node.type === 'image'" title="添加为参考图" @click.stop="emit('node-action', { type: 'ref' })">＋</button>
      <button class="cv-tools__del" title="删除" @click.stop="emit('node-action', { type: 'delete' })">✕</button>
    </div>
    <div class="cv-node__clip">
    <!-- 图片节点 -->
    <template v-if="node.type === 'image'">
      <img
        v-if="!imgBroken && imgBlob"
        class="cv-node__img"
        :src="imgBlob"
        decoding="async"
        draggable="false"
        alt=""
        @load="onImgLoad"
      >
      <div v-else class="cv-node__missing">图片不可用<small v-if="imgStatus">HTTP {{ imgStatus }}</small></div>
      <div v-if="node.prompt" class="cv-node__cap" :title="node.prompt">{{ node.prompt }}</div>
    </template>

    <!-- 视频节点 -->
    <template v-else-if="node.type === 'video'">
      <video
        v-if="!imgBroken && imgBlob"
        class="cv-node__img"
        :src="imgBlob"
        controls
        muted
        loop
        playsinline
        @loadedmetadata="onVidMeta"
        @error="imgBroken = true"
      />
      <div v-else class="cv-node__missing">{{ imgBroken ? '视频不可用' : '加载中…' }}<small v-if="imgStatus">HTTP {{ imgStatus }}</small></div>
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
        <span class="cv-node__genlabel">生成中…</span>
        <span class="cv-node__gensub">{{ node.model || 'AI' }}</span>
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
  border-radius: 16px;
  border: 1px solid rgb(24 24 27 / 0.08);
  contain: layout style;
  user-select: none;
  touch-action: none;
  cursor: grab;
  background: var(--surface-solid);
  box-shadow: 0 1px 2px rgb(24 24 27 / 0.05), 0 8px 24px -8px rgb(24 24 27 / 0.12);
  transition: box-shadow 0.15s ease, border-color 0.15s ease;
}
.cv-node__clip {
  position: absolute;
  inset: 0;
  border-radius: 13px;
  overflow: hidden;
}
.cv-node.is-selected {
  border-color: #18181b;
  box-shadow: 0 0 0 2px #18181b, 0 14px 34px -10px rgb(24 24 27 / 0.3);
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
  padding: 36px 14px 12px;
  font-size: 32px;
  font-weight: 600;
  line-height: 1.4;
  color: #fff;
  text-shadow: 0 1px 4px rgb(0 0 0 / 0.65);
  background: linear-gradient(to top, rgb(0 0 0 / 0.66), transparent);
  pointer-events: none;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
}

/* 笔记 = 白色卡片 */
.cv-node--note {
  background: #fff;
  border-color: rgb(24 24 27 / 0.1);
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
  padding: 12px 14px;
  font-size: 32px;
  line-height: 1.35;
  color: #27272a;
  outline: none;
  white-space: pre-wrap;
  word-break: break-word;
  overflow: hidden;
  user-select: none;
}
.is-editing .cv-node__note {
  user-select: text;
  cursor: text;
}

.cv-node--gen {
  border-style: dashed;
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 600;
}
.cv-node--gen.is-failed {
  border-color: #e5484d;
  color: #e5484d;
}
/* 生成占位内容:居中排布 + 微光扫过 */
.cv-node__gen {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  background:
    radial-gradient(130% 130% at 15% 0%, rgb(24 24 27 / 0.14), transparent 55%),
    radial-gradient(130% 130% at 85% 100%, rgb(24 24 27 / 0.12), transparent 55%),
    var(--surface);
}
.cv-node__gen::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(105deg, transparent 38%, rgb(255 255 255 / 0.18) 50%, transparent 62%);
  background-size: 260% 100%;
  background-position: 260% 0;
  animation: cv-gen-shine 2.4s linear infinite;
  pointer-events: none;
}
.is-failed .cv-node__gen {
  background:
    radial-gradient(130% 130% at 15% 0%, rgb(229 72 77 / 0.1), transparent 55%),
    var(--surface);
}
.is-failed .cv-node__gen::after {
  display: none;
}
.cv-node__genlabel {
  letter-spacing: 2px;
}
.cv-node__gensub {
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-faint);
}
.cv-node__spin {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: 3px solid rgb(24 24 27 / 0.2);
  border-top-color: var(--m-orange);
  border-right-color: var(--m-orange-2);
  animation: cv-spin 0.9s linear infinite;
  filter: drop-shadow(0 0 10px var(--accent-glow));
}
@keyframes cv-spin {
  to { transform: rotate(360deg); }
}
@keyframes cv-gen-shine {
  to { background-position: -260% 0; }
}
.cv-node__err {
  font-size: 11px;
  line-height: 1.5;
  max-width: min(90%, 320px);
  max-height: 5em;
  overflow: auto;
  overflow-wrap: anywhere;
  text-align: left;
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
  opacity: 0;
  transition: opacity 0.15s ease;
}
.cv-node:hover .cv-port,
.cv-node.is-selected .cv-port,
.cv-port.is-active,
.cv-port:focus-visible { opacity: 1; }
.cv-port::after {
  content: '';
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  border: 2px solid #18181b;
  box-shadow: 0 1px 4px rgb(0 0 0 / 0.25);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.cv-port:hover::after, .cv-port:focus-visible::after, .cv-port.is-active::after {
  transform: scale(1.3);
  box-shadow: 0 0 0 4px rgb(24 24 27 / 0.25), 0 2px 10px var(--accent-glow);
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

/* 悬浮工具条:鼠标放上节点就显示(选中时常显),贴节点内上缘 */
.cv-tools {
  position: absolute;
  top: 6px;
  right: 6px;
  display: flex;
  gap: 4px;
  z-index: 4;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.12s ease;
}
.cv-node:hover .cv-tools,
.cv-node.is-selected .cv-tools,
.cv-tools:focus-within {
  opacity: 1;
  pointer-events: auto;
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
