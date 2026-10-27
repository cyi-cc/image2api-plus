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
      <div class="w-full max-w-lg rounded-2xl bg-[var(--menu-bg)] backdrop-blur-xl text-[color:var(--fg)] border border-[color:var(--glass-border)] shadow-[var(--glass-shadow-lg)] overflow-hidden flex flex-col max-h-[80vh]">
        <div class="px-6 py-4 border-b border-b-[color:var(--ink-soft)] flex items-center justify-between gap-3 shrink-0">
          <div class="flex items-center gap-2">
            <span class="w-7 h-7 rounded-lg text-white grid place-items-center"
                  style="background: linear-gradient(135deg, #18181b, #52525b); box-shadow: 0 4px 12px var(--accent-glow)">
              <Icon name="spark" class="w-4 h-4" />
            </span>
            <h2 class="text-base font-extrabold">公告</h2>
          </div>
          <button @click="dismissAnnouncement" class="text-[color:var(--fg-3)] hover:text-[color:var(--fg)] transition-colors">
            <Icon name="close" class="w-5 h-5" />
          </button>
        </div>
        <div class="ann-body px-6 py-5 overflow-y-auto" v-html="html"></div>
        <div class="px-6 py-4 border-t border-t-[color:var(--ink-soft)] flex justify-end shrink-0">
          <button @click="dismissAnnouncement"
                  class="rounded-lg text-white border border-white/20 px-5 py-2 text-sm font-extrabold hover:-translate-y-0.5 active:translate-y-0.5 transition-all"
                  style="background: linear-gradient(120deg, #18181b, #52525b); box-shadow: 0 8px 20px var(--accent-glow)">
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
.ann-body { font-size: 0.9rem; line-height: 1.65; color: var(--fg-2); }
.ann-body :deep(h1) { font-size: 1.25rem; font-weight: 700; margin: 0.6em 0 0.4em; color: var(--fg); }
.ann-body :deep(h2) { font-size: 1.1rem; font-weight: 700; margin: 0.6em 0 0.4em; color: var(--fg); }
.ann-body :deep(h3) { font-size: 1rem; font-weight: 600; margin: 0.6em 0 0.3em; color: var(--fg); }
.ann-body :deep(p) { margin: 0.5em 0; }
.ann-body :deep(ul), .ann-body :deep(ol) { margin: 0.5em 0; padding-left: 1.4em; }
.ann-body :deep(ul) { list-style: disc; }
.ann-body :deep(ol) { list-style: decimal; }
.ann-body :deep(li) { margin: 0.2em 0; }
.ann-body :deep(a) { color: var(--m-orange); text-decoration: underline; }
.ann-body :deep(strong) { font-weight: 700; color: var(--fg); }
.ann-body :deep(code) { background: rgb(24 24 27 / 0.12); padding: 0.1em 0.35em; border-radius: 0.3rem; font-size: 0.85em; }
.ann-body :deep(pre) { background: #111; color: #f5f5f5; padding: 0.8em; border-radius: 0.5rem; overflow-x: auto; margin: 0.6em 0; }
.ann-body :deep(pre code) { background: none; padding: 0; }
.ann-body :deep(blockquote) { border-left: 3px solid var(--m-orange); padding-left: 0.9em; color: var(--fg-3); margin: 0.6em 0; }
.ann-body :deep(hr) { border: none; border-top: 1px solid var(--hairline); margin: 1em 0; }
.ann-body :deep(img) { max-width: 100%; border-radius: 0.5rem; margin: 0.5em 0; }
</style>
