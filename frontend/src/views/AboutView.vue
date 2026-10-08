<script setup>
import { computed } from 'vue'
import { site } from '../site'

const contact = computed(() => site.contact || {})

const powers = [
  { icon: '🎨', title: '文字生图', desc: '一句话出图，画幅与分辨率随模型可选。', bg: 'var(--m-mint)' },
  { icon: '🖼️', title: '参考图生图', desc: '上传参考换风格、改构图、做变体。', bg: 'var(--m-pink)' },
  { icon: '🎬', title: '文字生视频', desc: '一段描述生成短片，时长分辨率可选。', bg: 'var(--m-blue)' },
  { icon: '✨', title: '图生视频', desc: '给张参考图，让静态画面动起来。', bg: 'var(--m-yellow)' },
]
</script>

<template>
  <div class="space-y-16 pb-8">
    <!-- ===== hero ===== -->
    <section class="relative overflow-hidden rounded-[1.4rem] anime-card px-6 py-10 md:px-12 md:py-14">
      <div class="memphis-dots absolute inset-0 opacity-40 pointer-events-none"></div>
      <div class="relative grid md:grid-cols-[1.1fr_0.9fr] gap-8 items-center">
        <div>
          <span class="sticker">✦ 关于我们</span>
          <h1 class="mt-4 text-4xl md:text-5xl font-black tracking-tight text-[color:var(--fg)]">
            把创作交给 <span class="anime-grad-text">{{ site.title }}</span>
          </h1>
          <p class="text-[color:var(--fg-2)] mt-5 text-base md:text-lg leading-relaxed max-w-xl">
            {{ site.title }} 是一个聚合 GPT、Gemini、Firefly、Flux 等顶级模型的 AI 生图与生视频平台。
            把脑海里的画面写成一句话，剩下的交给我们变成图像与视频。
          </p>
          <router-link to="/user" class="bubble-btn mt-7">开始创作 →</router-link>
        </div>
        <!-- memphis tile cluster standing in for the old mascot illustration -->
        <div class="ab-cluster" aria-hidden="true">
          <span class="ab-tile ab-1">🎨</span>
          <span class="ab-tile ab-2">🖼️</span>
          <span class="ab-tile ab-3">🎬</span>
          <span class="ab-tile ab-4">✨</span>
          <span class="ab-star">✦</span>
        </div>
      </div>
    </section>

    <!-- ===== powers ===== -->
    <section>
      <div class="anime-eyebrow text-[var(--m-orange)]">能做什么</div>
      <h2 class="mt-3 text-2xl md:text-3xl font-black tracking-tight text-[color:var(--fg)]">四种方式，把想象变成画面</h2>
      <div class="mt-7 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <div v-for="p in powers" :key="p.title"
             class="power-card p-5" :style="{ background: p.bg }">
          <div class="text-3xl">{{ p.icon }}</div>
          <div class="mt-3 text-base font-extrabold text-[#111]">{{ p.title }}</div>
          <div class="mt-1.5 text-[13px] font-medium leading-relaxed text-[#111]/60">{{ p.desc }}</div>
        </div>
      </div>
    </section>

    <!-- ===== contact ===== -->
    <section>
      <div class="anime-eyebrow text-sky-600">联系我们</div>
      <h2 class="mt-3 text-2xl md:text-3xl font-black tracking-tight text-[color:var(--fg)]">有问题？来找我们玩～</h2>
      <p class="text-[color:var(--fg-3)] mt-2 mb-6">合作、反馈，或想加入交流群，随时招呼。</p>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 max-w-3xl">
        <a v-if="contact.shop" :href="contact.shop" target="_blank" rel="noopener"
           class="group anime-card flex items-center justify-between gap-3 p-5">
          <div>
            <div class="sticker">🛍️ 商店</div>
            <div class="text-base font-semibold text-[color:var(--fg)] mt-2 group-hover:text-[var(--m-orange)] transition-colors">前往商店</div>
          </div>
          <span class="text-[color:var(--fg-faint)] group-hover:translate-x-1 transition-transform">→</span>
        </a>

        <a v-if="contact.email" :href="`mailto:${contact.email}`"
           class="group anime-card p-5">
          <div class="sticker">✉️ 邮箱</div>
          <div class="text-base font-semibold text-[color:var(--fg)] mt-2 break-all group-hover:text-sky-600 transition-colors">{{ contact.email }}</div>
        </a>

        <a v-if="contact.qq && contact.qq_link" :href="contact.qq_link" target="_blank" rel="noopener"
           class="group anime-card flex items-center justify-between gap-3 p-5">
          <div>
            <div class="sticker">💬 QQ</div>
            <div class="text-base font-semibold text-[color:var(--fg)] mt-2 tabular-nums break-all group-hover:text-sky-600 transition-colors">{{ contact.qq }}</div>
          </div>
          <span class="text-[color:var(--fg-faint)] group-hover:translate-x-1 transition-transform">→</span>
        </a>
        <div v-else-if="contact.qq" class="anime-card p-5">
          <div class="sticker">💬 QQ</div>
          <div class="text-base font-semibold text-[color:var(--fg)] mt-2 tabular-nums break-all">{{ contact.qq }}</div>
        </div>

        <a v-if="contact.qq_group && contact.qq_group_link" :href="contact.qq_group_link" target="_blank" rel="noopener"
           class="group anime-card flex items-center justify-between gap-3 p-5">
          <div>
            <div class="sticker">🎀 QQ 群</div>
            <div class="text-base font-semibold text-[color:var(--fg)] mt-2 tabular-nums break-all group-hover:text-emerald-600 transition-colors">{{ contact.qq_group }}</div>
          </div>
          <span class="text-[color:var(--fg-faint)] group-hover:translate-x-1 transition-transform">→</span>
        </a>
        <div v-else-if="contact.qq_group" class="anime-card p-5">
          <div class="sticker">🎀 QQ 群</div>
          <div class="text-base font-semibold text-[color:var(--fg)] mt-2 tabular-nums break-all">{{ contact.qq_group }}</div>
        </div>
      </div>

      <p v-if="!contact.shop && !contact.email && !contact.qq && !contact.qq_group"
         class="text-sm text-[color:var(--fg-3)]">管理员尚未配置联系方式。</p>
    </section>
  </div>
</template>

<style scoped>
/* Crayon tile cluster — a loose collage of rotated ink-outlined squares. */
.ab-cluster {
  position: relative;
  min-height: 240px;
}
.ab-tile {
  position: absolute;
  display: grid;
  place-items: center;
  width: 5.5rem;
  height: 5.5rem;
  font-size: 2.2rem;
  border: 3px solid var(--ink);
  border-radius: 1rem;
  box-shadow: 6px 6px 0 var(--shadow-ink);
}
.ab-1 { left: 12%; top: 6%; background: var(--m-mint); transform: rotate(-9deg); animation: memphis-float 6s ease-in-out infinite; }
.ab-2 { right: 14%; top: 0; background: var(--m-pink); transform: rotate(7deg); animation: memphis-float 7s ease-in-out infinite reverse; }
.ab-3 { left: 26%; bottom: 4%; background: var(--m-blue); transform: rotate(5deg); animation: memphis-bounce 4.5s ease-in-out infinite; }
.ab-4 { right: 8%; bottom: 14%; background: var(--m-yellow); transform: rotate(-6deg); animation: memphis-float 5.5s ease-in-out infinite; }
.ab-star {
  position: absolute;
  left: 50%; top: 44%;
  font-size: 2.6rem;
  color: var(--m-orange);
  animation: memphis-wiggle 3.5s ease-in-out infinite;
}
.power-card {
  border: 2.5px solid var(--ink);
  border-radius: 1rem;
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: transform 0.18s ease, box-shadow 0.18s ease;
}
.power-card:hover { transform: translate(-2px, -2px); box-shadow: 8px 8px 0 var(--shadow-ink); }
@media (max-width: 768px) { .ab-cluster { display: none; } }
@media (prefers-reduced-motion: reduce) { .ab-tile, .ab-star { animation: none; } }
</style>
