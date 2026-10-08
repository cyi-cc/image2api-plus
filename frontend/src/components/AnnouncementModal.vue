<script setup>
import { computed } from 'vue'
import { marked } from 'marked'
import { announcement, dismissAnnouncement } from '../announcement'
import Icon from './Icon.vue'

marked.setOptions({ breaks: true, gfm: true })
const html = computed(() => marked.parse(announcement.content.trim() || '暂无公告'))
</script>

<template>
  <transition name="ann-fade">
    <div v-if="announcement.show"
         class="fixed inset-0 z-[80] bg-slate-950/70 backdrop-blur-sm flex items-center justify-center p-4"
         @click.self="dismissAnnouncement">
      <div class="w-full max-w-lg rounded-xl bg-white text-slate-800 border-[3px] border-[#111] shadow-[8px_8px_0_#111] overflow-hidden flex flex-col max-h-[80vh]">
        <div class="px-6 py-4 border-b-[2.5px] border-b-[#111] flex items-center justify-between gap-3 shrink-0">
          <div class="flex items-center gap-2">
            <span class="w-7 h-7 rounded-lg bg-[#fef08a] text-[#111] border-2 border-[#111] grid place-items-center">
              <Icon name="spark" class="w-4 h-4" />
            </span>
            <h2 class="text-base font-extrabold">公告</h2>
          </div>
          <button @click="dismissAnnouncement" class="text-slate-400 hover:text-slate-700 transition-colors">
            <Icon name="close" class="w-5 h-5" />
          </button>
        </div>
        <div class="ann-body px-6 py-5 overflow-y-auto" v-html="html"></div>
        <div class="px-6 py-4 border-t-[2.5px] border-t-[#111] flex justify-end shrink-0">
          <button @click="dismissAnnouncement"
                  class="rounded-lg bg-[#ec5b13] text-white border-[2.5px] border-[#111] shadow-[3px_3px_0_#111] hover:-translate-x-0.5 hover:-translate-y-0.5 hover:shadow-[4px_4px_0_#111] active:translate-y-0.5 active:shadow-none px-5 py-2 text-sm font-extrabold transition-all">
            我知道了
          </button>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.ann-fade-enter-active, .ann-fade-leave-active { transition: opacity 0.2s ease; }
.ann-fade-enter-from, .ann-fade-leave-to { opacity: 0; }

/* Minimal markdown typography for the announcement body. */
.ann-body { font-size: 0.9rem; line-height: 1.65; color: rgb(51 65 85); }
.ann-body :deep(h1) { font-size: 1.25rem; font-weight: 700; margin: 0.6em 0 0.4em; color: rgb(15 23 42); }
.ann-body :deep(h2) { font-size: 1.1rem; font-weight: 700; margin: 0.6em 0 0.4em; color: rgb(15 23 42); }
.ann-body :deep(h3) { font-size: 1rem; font-weight: 600; margin: 0.6em 0 0.3em; color: rgb(15 23 42); }
.ann-body :deep(p) { margin: 0.5em 0; }
.ann-body :deep(ul), .ann-body :deep(ol) { margin: 0.5em 0; padding-left: 1.4em; }
.ann-body :deep(ul) { list-style: disc; }
.ann-body :deep(ol) { list-style: decimal; }
.ann-body :deep(li) { margin: 0.2em 0; }
.ann-body :deep(a) { color: var(--m-orange); text-decoration: underline; }
.ann-body :deep(strong) { font-weight: 700; color: rgb(15 23 42); }
.ann-body :deep(code) { background: rgb(254 240 138 / 0.5); padding: 0.1em 0.35em; border-radius: 0.3rem; font-size: 0.85em; }
.ann-body :deep(pre) { background: #111; color: #f5f5f5; padding: 0.8em; border-radius: 0.5rem; overflow-x: auto; margin: 0.6em 0; }
.ann-body :deep(pre code) { background: none; padding: 0; }
.ann-body :deep(blockquote) { border-left: 3px solid var(--m-orange); padding-left: 0.9em; color: rgb(100 116 139); margin: 0.6em 0; }
.ann-body :deep(hr) { border: none; border-top: 1px solid rgb(226 232 240); margin: 1em 0; }
.ann-body :deep(img) { max-width: 100%; border-radius: 0.5rem; margin: 0.5em 0; }
</style>
