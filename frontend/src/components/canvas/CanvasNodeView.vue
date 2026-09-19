<script setup>
// 单个画布节点。位置/尺寸由 stage 的世界层 transform 统一缩放,
// 这里只负责节点自身内容:image / note / gen(生成中占位)。
// 性能:contain 隔离布局;图片 lazy + async 解码 + 缩略图。
import { ref, onMounted } from 'vue'
import { thumbUrl } from '../../api'

const props = defineProps({
  node: { type: Object, required: true },
  selected: { type: Boolean, default: false },
})
const emit = defineEmits(['imgload', 'note-commit', 'node-action'])

const imgLoaded = ref(false)
const noteEl = ref(null)
const editing = ref(false)

onMounted(() => {
  // 非受控 contenteditable:只在挂载时写初值,避免输入时光标被 v-model 回写打跳。
  if (props.node.type === 'note' && noteEl.value) noteEl.value.innerText = props.node.text || ''
})

function onImgLoad(e) {
  imgLoaded.value = true
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
  if (!editing.value) return
  editing.value = false
  const text = (noteEl.value?.innerText || '').slice(0, 4000)
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
    <!-- 图片节点 -->
    <template v-if="node.type === 'image'">
      <img
        class="cv-node__img"
        :src="thumbUrl(node.src)"
        loading="lazy"
        decoding="async"
        draggable="false"
        alt=""
        @load="onImgLoad"
        @error="imgLoaded = true"
      >
      <div v-if="node.prompt" class="cv-node__cap" :title="node.prompt">{{ node.prompt }}</div>
    </template>

    <!-- 笔记节点 -->
    <div
      v-else-if="node.type === 'note'"
      ref="noteEl"
      class="cv-node__note"
      contenteditable="true"
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
        <span class="cv-node__err" :title="node.error || ''">生成失败</span>
      </template>
    </div>
  </div>
</template>

<style scoped>
.cv-node {
  position: absolute;
  border-radius: 10px;
  overflow: hidden;
  contain: layout style paint;
  user-select: none;
  touch-action: none;
  cursor: grab;
  box-shadow: 0 2px 10px rgb(0 0 0 / 0.35);
}
.cv-node.is-selected {
  outline: 2px solid #7c6cff;
  outline-offset: 2px;
}
.cv-node:active {
  cursor: grabbing;
}

.cv-node__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  background: #14161d;
}

.cv-node__cap {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  padding: 14px 8px 5px;
  font-size: 11px;
  line-height: 1.35;
  color: #d6d9e2;
  background: linear-gradient(transparent, rgb(8 9 13 / 0.92));
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  pointer-events: none;
}

.cv-node--note {
  background: #23262f;
  border: 1px solid #343947;
  cursor: text;
}
.cv-node--note:not(.is-editing) {
  cursor: grab;
}
.cv-node__note {
  width: 100%;
  height: 100%;
  padding: 10px 12px;
  font-size: 13px;
  line-height: 1.5;
  color: #e8eaf0;
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
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  background: #1a1d26;
  border: 1.5px dashed #4a4f61;
  color: #9aa1b2;
  font-size: 13px;
}
.cv-node--gen.is-failed {
  border-color: #6b3b44;
  color: #e58c96;
}
.cv-node__spin {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2.5px solid #3b4152;
  border-top-color: #7c6cff;
  animation: cv-spin 0.9s linear infinite;
}
@keyframes cv-spin {
  to { transform: rotate(360deg); }
}
.cv-node__err {
  font-size: 11px;
  max-width: 90%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
