<script setup>
import { computed } from 'vue'
import { site } from '../site'

const contact = computed(() => site.contact || {})

const powers = [
  { icon: 't2i', title: '文字生图', desc: '一句话出图，画幅与分辨率随模型可选。', tint: 'linear-gradient(135deg,#34d399,#2dd4bf)' },
  { icon: 'i2i', title: '参考图生图', desc: '上传参考换风格、改构图、做变体。', tint: 'linear-gradient(135deg,#e879f9,#f472b6)' },
  { icon: 't2v', title: '文字生视频', desc: '一段描述生成短片，时长分辨率可选。', tint: 'linear-gradient(135deg,#38bdf8,#52525b)' },
  { icon: 'i2v', title: '图生视频', desc: '给张参考图，让静态画面动起来。', tint: 'linear-gradient(135deg,#18181b,#3f3f46)' },
]

const contactCards = computed(() => {
  const c = contact.value || {}
  const list = []
  if (c.shop) list.push({ label: '商店', value: '前往商店', href: c.shop })
  if (c.email) list.push({ label: '邮箱', value: c.email, href: `mailto:${c.email}` })
  if (c.qq) list.push({ label: 'QQ', value: c.qq, href: c.qq_link || '' })
  if (c.qq_group) list.push({ label: 'QQ 群', value: c.qq_group, href: c.qq_group_link || '' })
  return list
})
</script>

<template>
  <div class="space-y-14 pb-8">
    <!-- ===== hero ===== -->
    <section class="ab-hero">
      <div class="relative grid md:grid-cols-[1.1fr_0.9fr] gap-8 items-center">
        <div>
          <span class="ab-eyebrow">✦ 关于我们</span>
          <h1 class="ab-title">把创作交给 <span class="ab-brand">{{ site.title }}</span></h1>
          <p class="ab-lead">
            {{ site.title }} 是一个 AI 生图与生视频平台，汇集多家顶级模型，按实际生成量计费。
            把脑海里的画面写成一句话，剩下的交给我们变成图像与视频。
          </p>
          <router-link to="/user" class="btn-primary ab-cta">开始创作 <span aria-hidden="true">→</span></router-link>
        </div>
        <!-- floating glass tile cluster standing in for a mascot illustration -->
        <div class="ab-cluster" aria-hidden="true">
          <span class="ab-tile ab-1"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l1.9 5.8a2 2 0 0 0 1.3 1.3L21 12l-5.8 1.9a2 2 0 0 0-1.3 1.3L12 21l-1.9-5.8a2 2 0 0 0-1.3-1.3L3 12l5.8-1.9a2 2 0 0 0 1.3-1.3z"/></svg></span>
          <span class="ab-tile ab-2"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 16 5-5 3 3 4-4 6 6"/><circle cx="9" cy="9" r="1.4"/></svg></span>
          <span class="ab-tile ab-3"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h13v12H3z"/><path d="M16 10l5-3v10l-5-3"/></svg></span>
          <span class="ab-tile ab-4"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h7l-1 8 10-12h-7z"/></svg></span>
          <span class="ab-star">✦</span>
        </div>
      </div>
    </section>

    <!-- ===== powers ===== -->
    <section>
      <div class="ab-eyebrow">能做什么</div>
      <h2 class="ab-h2">四种方式，把想象变成画面</h2>
      <div class="mt-6 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <div v-for="p in powers" :key="p.title" class="power-card p-5">
          <div class="power-ico" :style="{ background: p.tint }">
            <svg v-if="p.icon === 't2i'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5h16v14H4z"/><path d="M4 15l4.5-4.5 3 3L15 10l5 5"/><circle cx="9" cy="9" r="1.3"/></svg>
            <svg v-else-if="p.icon === 'i2i'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7h11v11H3z"/><path d="M10 4h11v11"/></svg>
            <svg v-else-if="p.icon === 't2v'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h13v12H3z"/><path d="M16 10l5-3v10l-5-3"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5h16v14H4z"/><path d="M9 9l6 3-6 3z"/></svg>
          </div>
          <div class="mt-3 text-base font-extrabold text-[color:var(--fg)]">{{ p.title }}</div>
          <div class="mt-1.5 text-[13px] font-medium leading-relaxed text-[color:var(--fg-3)]">{{ p.desc }}</div>
        </div>
      </div>
    </section>

    <!-- ===== contact ===== -->
    <section>
      <div class="ab-eyebrow">联系我们</div>
      <h2 class="ab-h2">有问题？来找我们玩～</h2>
      <p class="ab-sub">合作、反馈，或想加入交流群，随时招呼。</p>

      <div class="mt-6 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <component :is="c.href ? 'a' : 'div'" v-for="c in contactCards" :key="c.label"
                   :href="c.href || undefined" :target="c.href ? '_blank' : undefined" rel="noopener"
                   class="ct-card" :class="c.href && 'ct-link'">
          <div class="ct-top">
            <span class="ct-label">{{ c.label }}</span>
            <span v-if="c.href" class="ct-arrow" aria-hidden="true">↗</span>
          </div>
          <div class="ct-value">{{ c.value }}</div>
        </component>
      </div>

      <p v-if="!contactCards.length" class="ab-sub">管理员尚未配置联系方式。</p>
    </section>
  </div>
</template>

<style scoped>
.ab-hero { position: relative; overflow: hidden; border-radius: 24px; padding: 48px 52px; background: var(--surface-solid); border: 1px solid rgb(24 24 27 / .07); box-shadow: 0 1px 2px rgb(24 24 27 / .03), 0 18px 40px -22px rgb(24 24 27 / .16); }
.ab-hero::before { content: ''; position: absolute; inset: 0; pointer-events: none; background: radial-gradient(520px 300px at 92% 0%, rgba(251, 146, 60, .1), transparent 70%); }
.ab-eyebrow { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; font-weight: 800; letter-spacing: .08em; color: var(--fg-3); }
.ab-title { margin-top: 14px; font-size: clamp(32px, 4vw, 48px); font-weight: 800; line-height: 1.15; letter-spacing: -0.04em; color: var(--fg); }
.ab-brand { background: linear-gradient(100deg, #18181b, #71717a); -webkit-background-clip: text; background-clip: text; color: transparent; }
.ab-lead { margin-top: 18px; max-width: 34em; font-size: 16px; line-height: 1.8; color: var(--fg-2); }
.ab-cta { margin-top: 26px; height: 44px; padding: 0 24px !important; font-size: 14px !important; border-radius: 999px !important; }
.ab-h2 { margin-top: 8px; font-size: 26px; font-weight: 800; letter-spacing: -0.03em; color: var(--fg); }
.ab-sub { margin-top: 6px; font-size: 14px; color: var(--fg-3); }
.ct-card { display: flex; flex-direction: column; justify-content: space-between; gap: 18px; min-height: 108px; padding: 18px 20px; border-radius: 16px; background: var(--surface-solid); border: 1px solid rgb(24 24 27 / .07); text-decoration: none; transition: transform .18s ease, border-color .18s ease, box-shadow .18s ease; }
.ct-link:hover { transform: translateY(-3px); border-color: rgb(24 24 27 / .3); box-shadow: 0 14px 30px -16px rgb(24 24 27 / .25); }
.ct-top { display: flex; align-items: center; justify-content: space-between; }
.ct-label { font-size: 12px; font-weight: 800; letter-spacing: .06em; color: var(--fg-3); }
.ct-arrow { font-size: 15px; color: var(--fg-faint); transition: transform .18s ease, color .18s ease; }
.ct-link:hover .ct-arrow { transform: translate(2px, -2px); color: var(--fg); }
.ct-value { font-size: 17px; font-weight: 800; letter-spacing: -0.01em; color: var(--fg); font-variant-numeric: tabular-nums; word-break: break-all; }
@media (max-width: 640px) { .ab-hero { padding: 30px 22px; } }
/* Floating glass tile cluster — a loose collage of rotated frosted squares. */
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
  border: 1px solid rgb(24 24 27 / .07);
  border-radius: 1.2rem;
  background: var(--surface-solid);
  box-shadow: 0 14px 30px -14px rgb(24 24 27 / .25);
  color: var(--m-orange);
}
.ab-tile svg { width: 2.3rem; height: 2.3rem; }
.ab-1 { left: 12%; top: 6%; transform: rotate(-9deg); animation: memphis-float 6s ease-in-out infinite; }
.ab-2 { right: 14%; top: 0; color: #18181b; transform: rotate(7deg); animation: memphis-float 7s ease-in-out infinite reverse; }
.ab-3 { left: 26%; bottom: 4%; color: #52525b; transform: rotate(5deg); animation: memphis-bounce 4.5s ease-in-out infinite; }
.ab-4 { right: 8%; bottom: 14%; color: #18181b; transform: rotate(-6deg); animation: memphis-float 5.5s ease-in-out infinite; }
.ab-star {
  position: absolute;
  left: 50%; top: 44%;
  font-size: 2.6rem;
  color: var(--m-orange);
  animation: memphis-wiggle 3.5s ease-in-out infinite;
}
.power-card {
  border: 1px solid rgb(24 24 27 / .07);
  border-radius: 1.1rem;
  background: #fff;
  box-shadow: 0 1px 2px rgb(24 24 27 / .03), 0 10px 28px -16px rgb(24 24 27 / .12);
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}
.power-card:hover { transform: translateY(-3px); border-color: rgb(24 24 27 / 0.45); box-shadow: var(--glass-shadow-lg); }
.power-ico {
  width: 2.6rem;
  height: 2.6rem;
  border-radius: 0.75rem;
  display: grid;
  place-items: center;
  color: #fff;
  box-shadow: 0 6px 16px rgb(24 24 27 / 0.28);
}
.power-ico svg { width: 1.3rem; height: 1.3rem; }
@media (max-width: 768px) { .ab-cluster { display: none; } }
@media (prefers-reduced-motion: reduce) { .ab-tile, .ab-star { animation: none; } }
</style>
