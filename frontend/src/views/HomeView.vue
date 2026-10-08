<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, generatedUrl } from '../api'
import { site } from '../site'
import { isAuthed, openLogin } from '../auth'
const router = useRouter()

// Navigate to a signed-in page, or pop the login modal (remembering the
// destination) when the visitor isn't logged in yet.
function go(path, query) {
  const target = query ? { path, query } : { path }
  if (isAuthed()) { router.push(target); return }
  openLogin(router.resolve(target).fullPath)
}

const stats = ref({ generated_count: 0, recent: [] })
const managed = ref([])     // managed model records (type, resolutions, ...)
const showcase = ref({ hero: [], bento: [], work: [] })
// heroDeck holds the top-3 hero cards in a RANDOMIZED order, so a different card
// fronts the deck on each page load. It's reshuffled only when the hero set first
// loads or its members change — not on every 30s poll, so the deck stays put.
const heroDeck = ref([])
function shuffleArr(arr) {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}
let timer = null

async function refresh() {
  try {
    // No /logs here — that endpoint requires a login and only returns the
    // caller's own entries. The latency KPI comes from /stats (aggregate,
    // prompt-free) so the public home page exposes nothing per-user.
    const [s, m, sc] = await Promise.all([
      api('/stats'),
      api('/managed-models'),
      api('/showcase'),
    ])
    stats.value = s.data || { generated_count: 0, recent: [] }
    managed.value = (m.data?.data || []).filter((x) => x.enabled !== false)
    showcase.value = sc.data?.data || { hero: [], bento: [], work: [] }
    const top3 = (showcase.value.hero || []).slice(0, 3)
    const sameSet = heroDeck.value.length === top3.length
      && top3.every((c) => heroDeck.value.some((d) => d.id === c.id))
    if (!sameSet) heroDeck.value = shuffleArr(top3)
  } catch {}
}
onMounted(() => { refresh(); timer = setInterval(refresh, 30000) })
onUnmounted(() => clearInterval(timer))

// ---- showcase groups: all three kinds are used ----
const bento = computed(() => showcase.value.bento || [])
const works = computed(() => showcase.value.work || [])
// hero + bento imagery, flattened into one strip for the scrolling gallery
// marquee under the hero. Duplicated inline in the template so the loop reads
// seamlessly when it wraps around.
const gallery = computed(() => {
  const seen = new Set()
  const out = []
  for (const c of [...(showcase.value.hero || []), ...bento.value]) {
    if (c && !seen.has(c.id)) { seen.add(c.id); out.push(c) }
  }
  return out
})

// ---- KPI strip — real signals only ----
const imageModels = computed(() => managed.value.filter((m) => m.type !== 'video'))
const videoModels = computed(() => managed.value.filter((m) => m.type === 'video'))
const modelCount = computed(() => managed.value.length)

// Resolution range across image models, e.g. "1K–4K" (or a single tier).
const RES_ORDER = ['1K', '2K', '4K']
const resRange = computed(() => {
  const seen = new Set()
  for (const m of imageModels.value) for (const r of m.resolutions || []) seen.add(r)
  const tiers = RES_ORDER.filter((r) => seen.has(r))
  if (!tiers.length) return '—'
  return tiers.length === 1 ? tiers[0] : `${tiers[0]}–${tiers[tiers.length - 1]}`
})

// Show the 24h average (matches the admin overview); fall back to the all-time
// average on a quiet day so the KPI isn't blank.
const avgElapsed = computed(() => stats.value?.avg_elapsed_ms_24h ?? stats.value?.avg_elapsed_ms ?? null)
const avgLabel = computed(() => {
  if (avgElapsed.value == null) return '—'
  if (avgElapsed.value < 1000) return avgElapsed.value + 'ms'
  return Math.round(avgElapsed.value / 1000) + 's'
})

// ---- capability cards, derived from the enabled models ----
function refs(m) { return m.image_to_image === true || Number(m.max_reference_images || 0) > 0 }
const durationRange = computed(() => {
  const secs = []
  for (const m of videoModels.value) {
    for (const d of m.durations || []) {
      const n = parseInt(String(d), 10)
      if (Number.isFinite(n)) secs.push(n)
    }
  }
  if (!secs.length) return ''
  const lo = Math.min(...secs), hi = Math.max(...secs)
  return lo === hi ? `${lo}s` : `${lo}–${hi}s`
})
const videoRes = computed(() => {
  const seen = new Set()
  for (const m of videoModels.value) for (const r of m.resolutions || []) seen.add(r)
  return [...seen].sort().join(' / ')
})
// Each card deep-links into the workbench with ?model=<id> (the only intent
// PlaygroundView reads besides ?prompt), picking the first model that actually
// offers that capability — a card with no model behind it is dropped.
const caps = computed(() => {
  const i2iModels = imageModels.value.filter(refs)
  const i2vModels = videoModels.value.filter(refs)
  return [
    {
      key: 't2i', title: '文字生图', model: imageModels.value[0]?.id,
      desc: '一句话出图，画幅与分辨率按所选模型的可用范围给出。',
      tint: '#a7f3d0', color: '#065f46',
      metas: [resRange.value, `${imageModels.value.length} 个模型`].filter(Boolean),
    },
    {
      key: 'i2i', title: '参考图生图', model: i2iModels[0]?.id,
      desc: '上传参考图换风格、改构图、做变体，支持多张参考。',
      tint: '#ffc1cc', color: '#9f1239',
      metas: ['多图参考', `${i2iModels.length} 个模型`],
    },
    {
      key: 't2v', title: '文字生视频', model: videoModels.value[0]?.id,
      desc: '一段描述生成短片，时长与分辨率按模型能力可选。',
      tint: '#bae6fd', color: '#075985',
      metas: [durationRange.value, videoRes.value, `${videoModels.value.length} 个模型`].filter(Boolean),
    },
    {
      key: 'i2v', title: '图生视频', model: i2vModels[0]?.id,
      desc: '给参考图或首帧，让静态画面动起来。',
      tint: '#fef08a', color: '#92400e',
      metas: ['参考图', `${i2vModels.length} 个模型`],
    },
  ].filter((c) => c.model)
})

// ---- model cards: the admin-set alias wins, so no upstream naming leaks ----
const MODEL_TINT = ['#ec5b13', '#ffc1cc', '#bae6fd', '#a7f3d0', '#fef08a', '#f472b6', '#94a3b8']
const modelCards = computed(() => managed.value.map((m, i) => {
  const bits = []
  if (m.resolutions?.length) bits.push(m.resolutions.join(' / '))
  if (m.type === 'video' && m.durations?.length) {
    const lo = parseInt(m.durations[0], 10)
    const hi = parseInt(m.durations[m.durations.length - 1], 10)
    bits.push(lo === hi ? `${lo}s` : `${lo}–${hi}s`)
  }
  if (m.type !== 'video') bits.push(refs(m) ? '支持参考图' : '文字生图')
  return {
    id: m.id,
    name: m.alias || m.name || m.id,
    spec: bits.join(' · '),
    video: m.type === 'video',
    glow: MODEL_TINT[i % MODEL_TINT.length],
  }
}))

// Resolve an image reference: external URLs pass through, relative paths
// (like "user/abc.png") are served from /images by the backend.
function imgSrc(image) {
  if (!image) return ''
  return /^https?:\/\//i.test(image) ? image : generatedUrl(image)
}
// Background style for a showcase card. Prefers a real image (the new shape);
// falls back to the legacy CSS gradient so seed entries still render.
// background-image (not <img>) keeps Edge's 视觉搜索 overlay off the artwork.
function cardBg(card) {
  if (card.image) {
    return {
      backgroundImage: `url("${imgSrc(card.image)}")`,
      backgroundSize: 'cover',
      backgroundPosition: 'center',
    }
  }
  return { background: card.gradient }
}

const prompt = ref('')
function submitPrompt() {
  const p = prompt.value.trim()
  go('/user', p ? { prompt: p } : null)
}
function useExample(ex) {
  go('/user', ex.prompt ? { prompt: ex.prompt } : null)
}
</script>

<template>
  <div class="px-4 md:px-8">
    <!-- ============ HERO (centered, memphis deco) ============ -->
    <section class="hero-band relative overflow-hidden pt-14 md:pt-20 pb-12">
      <div class="anime-stars"></div>

      <!-- floating memphis shapes (desktop only) -->
      <div aria-hidden="true" class="pointer-events-none absolute inset-0 hidden lg:block">
        <span class="deco deco-a">🎨</span>
        <span class="deco deco-b">⚡</span>
        <span class="deco deco-c">4K</span>
        <span class="deco deco-d">🎬</span>
        <span class="deco deco-star">✦</span>
        <span class="deco deco-star deco-star-2">✦</span>
      </div>

      <div class="relative flex flex-col items-center text-center">
        <span class="sticker">
          <span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 border border-[#111]"></span>
          {{ modelCount }} 个模型在线 · 图像与视频一站生成
        </span>

        <h1 class="mt-5 font-black tracking-tight leading-[1.05] text-[clamp(2.6rem,6vw,4.6rem)] text-[color:var(--fg)]">
          把想象 <span class="anime-grad-text">说给 AI 听</span>
        </h1>

        <p class="mt-5 text-[15px] md:text-base font-medium text-[color:var(--fg-2)] max-w-xl leading-relaxed">
          写一句话，剩下的交给我们：自动选模型、自动排队，图像与视频都在同一个工作台里出片。
        </p>

        <!-- prompt composer — the shortest path into the workbench -->
        <form class="composer mt-9 !max-w-2xl w-full" @submit.prevent="submitPrompt">
          <input v-model="prompt" class="composer-input" type="text"
                 placeholder="描述你想要的画面，比如：雨夜的东京小巷，霓虹倒映在湿滑路面…" />
          <button type="submit" class="composer-go">✦ 生成</button>
        </form>

        <!-- quick starters come from the curated bento set -->
        <div v-if="bento.length" class="mt-5 flex flex-wrap justify-center gap-2 max-w-2xl">
          <button v-for="ex in bento.slice(0, 5)" :key="ex.id" type="button" class="chip"
                  @click="useExample(ex)">{{ ex.title }}</button>
        </div>
      </div>
    </section>

    <!-- ============ GALLERY MARQUEE (hero + bento imagery) ============ -->
    <section v-if="gallery.length" class="mt-6 -mx-4 md:-mx-8 overflow-hidden marquee-mask border-y-[3px] border-y-[color:var(--ink)] bg-[var(--surface)] py-4">
      <div class="marquee flex gap-4 px-4 md:px-8 w-max">
        <button v-for="(card, i) in [...gallery, ...gallery]" :key="card.id + '-' + i" type="button"
                class="gcard" :style="cardBg(card)" :aria-label="card.title" @click="useExample(card)">
          <span class="tile-veil"></span>
          <span class="tile-cap">
            <i>{{ card.subtitle }}</i>
            <b>{{ card.title }}</b>
          </span>
        </button>
      </div>
    </section>

    <!-- ============ KPI STRIP ============ -->
    <section class="mt-12 grid grid-cols-2 md:grid-cols-4 gap-3">
      <div v-for="k in [
              { v: modelCount, l: '在线模型', e: '🧩', bg: 'var(--m-yellow)' },
              { v: caps.length, l: '创作方式', e: '🎨', bg: 'var(--m-pink)' },
              { v: resRange, l: '输出分辨率', e: '🖼️', bg: 'var(--m-blue)' },
              { v: avgLabel, l: '平均出片 · 近 24h', e: '⚡', bg: 'var(--m-mint)' },
            ]" :key="k.l"
           class="kpi-card px-5 py-4" :style="{ background: k.bg }">
        <div class="flex items-center gap-2">
          <span class="text-lg">{{ k.e }}</span>
          <span class="text-[26px] font-black tabular-nums text-[#111]">{{ k.v }}</span>
        </div>
        <div class="mt-1.5 text-[10px] font-bold uppercase tracking-[0.18em] text-[#111]/60">{{ k.l }}</div>
      </div>
    </section>

    <!-- ============ CAPABILITIES ============ -->
    <section class="mt-24">
      <div class="anime-eyebrow text-[var(--m-orange)]">能力</div>
      <h2 class="mt-3 text-[clamp(1.65rem,3vw,2.3rem)] font-extrabold tracking-tight text-[color:var(--fg)]">
        {{ caps.length }} 种方式开始创作
      </h2>
      <p class="mt-2.5 text-[13.5px] text-[color:var(--fg-3)] max-w-lg leading-relaxed">
        分辨率与时长直接读取模型配置，页面上写的就是你实际能选的。
      </p>

      <div class="mt-8 grid sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <button v-for="c in caps" :key="c.key" type="button" class="cap-card"
                @click="go('/user', { model: c.model })">
          <span class="cap-arrow">↗</span>
          <span class="cap-icon" :style="{ background: c.tint, color: c.color }">
            <svg v-if="c.key === 't2i'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 5h16v14H4z"/><path d="M4 15l4.5-4.5 3 3L15 10l5 5"/><circle cx="9" cy="9" r="1.3"/></svg>
            <svg v-else-if="c.key === 'i2i'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 7h11v11H3z"/><path d="M10 4h11v11"/></svg>
            <svg v-else-if="c.key === 't2v'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 6h13v12H3z"/><path d="M16 10l5-3v10l-5-3"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 5h16v14H4z"/><path d="M9 9l6 3-6 3z"/></svg>
          </span>
          <span class="block text-[15.5px] font-bold text-[color:var(--fg)]">{{ c.title }}</span>
          <span class="mt-2 block text-[12.5px] leading-relaxed text-[color:var(--fg-3)]">{{ c.desc }}</span>
          <span class="mt-3.5 flex flex-wrap gap-1.5">
            <i v-for="mt in c.metas" :key="mt" class="cap-meta">{{ mt }}</i>
          </span>
        </button>
      </div>
    </section>

    <!-- ============ INSPIRATION (kind=bento) ============ -->
    <section v-if="bento.length" id="bento" class="mt-24">
      <div class="anime-eyebrow text-rose-500">灵感</div>
      <h2 class="mt-3 text-[clamp(1.65rem,3vw,2.3rem)] font-extrabold tracking-tight text-[color:var(--fg)]">点一下，套用同款提示词</h2>
      <p class="mt-2.5 text-[13.5px] text-[color:var(--fg-3)] max-w-lg leading-relaxed">
        任选一张，自动进入画图工作台并预填提示词。
      </p>

      <div class="mt-8 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 grid-flow-row-dense gap-3 auto-rows-[170px]">
        <button v-for="ex in bento" :key="ex.id" type="button" class="tile" :class="ex.span"
                :style="cardBg(ex)" @click="useExample(ex)">
          <span class="tile-veil"></span>
          <span class="tile-cap">
            <i>{{ ex.subtitle }}</i>
            <b>{{ ex.title }}</b>
            <em>{{ ex.prompt }}</em>
          </span>
        </button>
      </div>
    </section>

    <!-- ============ WORKS (kind=work) ============ -->
    <section v-if="works.length" class="mt-24">
      <div class="anime-eyebrow text-sky-600">作品</div>
      <h2 class="mt-3 text-[clamp(1.65rem,3vw,2.3rem)] font-extrabold tracking-tight text-[color:var(--fg)]">大家都在生成什么</h2>
      <p class="mt-2.5 text-[13.5px] text-[color:var(--fg-3)] max-w-lg leading-relaxed">
        精选公开作品，点开即用同一条提示词继续创作。
      </p>

      <div class="mt-8 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <button v-for="w in works" :key="w.id" type="button" class="work" @click="useExample(w)">
          <span class="work-img" :style="cardBg(w)"></span>
          <span class="work-overlay">
            <b>{{ w.title }}</b>
            <em>{{ w.prompt }}</em>
            <i class="work-reuse">套用提示词</i>
          </span>
        </button>
      </div>
    </section>

    <!-- ============ MODELS ============ -->
    <section v-if="modelCards.length" class="mt-24">
      <div class="anime-eyebrow text-emerald-600">模型</div>
      <h2 class="mt-3 text-[clamp(1.65rem,3vw,2.3rem)] font-extrabold tracking-tight text-[color:var(--fg)]">在线模型</h2>
      <p class="mt-2.5 text-[13.5px] text-[color:var(--fg-3)] max-w-lg leading-relaxed">
        每个模型的分辨率、时长与参考图能力都写在卡片上，选之前就知道能做什么。
      </p>

      <div class="mt-8 flex gap-3 overflow-x-auto pb-2 model-row">
        <div v-for="m in modelCards" :key="m.id" class="model-card">
          <span class="model-glow" :style="{ background: m.glow }"></span>
          <div class="relative text-[15px] font-bold text-[color:var(--fg)]">{{ m.name }}</div>
          <div class="relative mt-1.5 text-[11px] text-[color:var(--fg-3)]">{{ m.spec }}</div>
          <div class="relative mt-3.5">
            <span :class="['model-tag', m.video ? 'is-video' : 'is-image']">{{ m.video ? '视频' : '图像' }}</span>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ CTA ============ -->
    <section class="mt-24 cta-band relative overflow-hidden rounded-[1.4rem] p-10 md:p-16">
      <div class="relative max-w-xl">
        <h3 class="text-[clamp(1.7rem,3.4vw,2.8rem)] font-extrabold tracking-tight leading-[1.2] text-[color:var(--fg)]">
          与其继续想，<br />不如现在写一句。
        </h3>
        <p class="mt-4 text-[15px] text-[color:var(--fg-2)] max-w-md leading-relaxed">
          注册即可开始，图像与视频都能试；不满意就换个模型，额度按次计费。
        </p>
        <button type="button" @click="go('/user')"
                class="mt-7 group inline-flex items-center gap-3 rounded-xl bg-[var(--m-orange)] text-white border-[3px] border-[color:var(--ink)] shadow-[5px_5px_0_var(--shadow-ink)] hover:shadow-[7px_7px_0_var(--shadow-ink)] hover:-translate-x-0.5 hover:-translate-y-0.5 active:translate-y-0.5 active:shadow-none pl-6 pr-3 py-3 text-sm font-extrabold transition-all">
          免费开始
          <span class="w-8 h-8 rounded-lg bg-white text-[#111] border-2 border-[#111] grid place-items-center font-black group-hover:translate-x-1 transition-transform">→</span>
        </button>
      </div>
    </section>

    <!-- thin footer line -->
    <footer class="mt-16 pt-10 border-t-[3px] border-t-[color:var(--ink)] flex flex-wrap items-center justify-between gap-4 text-xs font-semibold text-[color:var(--fg-3)]">
      <div class="flex items-center gap-3">
        <span class="font-semibold text-[color:var(--fg-2)]">{{ site.title }}</span>
        <span>·</span>
        <span>AI 生图与生视频平台</span>
      </div>
      <div class="flex items-center gap-4">
        <router-link to="/docs" class="hover:text-[color:var(--fg)] transition-colors">文档</router-link>
        <router-link to="/about" class="hover:text-[color:var(--fg)] transition-colors">关于</router-link>
      </div>
    </footer>
  </div>
</template>

<style scoped>
/* ---------- composer ---------- */
.composer {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  max-width: 36rem;
  padding: 0.5rem;
  border-radius: 1rem;
  background: var(--surface);
  border: 3px solid var(--ink);
  box-shadow: 7px 7px 0 var(--shadow-ink);
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}
.composer:focus-within {
  transform: translate(-2px, -2px);
  box-shadow: 10px 10px 0 var(--m-orange);
}
.composer-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--fg);
  font-size: 15px;
  font-weight: 500;
  padding: 0.7rem 0.85rem;
}
.composer-input::placeholder { color: var(--fg-faint); }
.composer-go {
  flex: none;
  border: 2.5px solid var(--ink);
  cursor: pointer;
  color: #fff;
  font-size: 14px;
  font-weight: 800;
  padding: 0.65rem 1.2rem;
  border-radius: 0.7rem;
  background: var(--m-orange);
  box-shadow: 3px 3px 0 var(--shadow-ink);
  transition: transform 0.15s ease, box-shadow 0.15s ease, filter 0.15s ease;
}
.composer-go:hover { transform: translate(-1px, -1px); box-shadow: 4px 4px 0 var(--shadow-ink); filter: brightness(1.06); }
.composer-go:active { transform: scale(0.96) translateY(2px); box-shadow: 0 0 0 var(--shadow-ink); }

.chip {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg-2);
  cursor: pointer;
  border: 2px solid var(--ink);
  padding: 0.4rem 0.8rem;
  border-radius: 0.6rem;
  background: var(--surface);
  transition: color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}
.chip:hover {
  color: #111;
  background: var(--m-yellow);
  box-shadow: 3px 3px 0 var(--shadow-ink);
  transform: translate(-1px, -1px);
}
html.dark .chip:hover { color: #111; }

/* ---------- hero cards (kind=hero) ---------- */
.fcard {
  position: absolute;
  border: 3px solid var(--ink);
  padding: 0;
  cursor: pointer;
  border-radius: 1rem;
  overflow: hidden;
  box-shadow: 7px 7px 0 var(--shadow-ink);
  transition: transform 0.45s cubic-bezier(0.2, 0.7, 0.2, 1);
  animation: fcardIn 0.7s cubic-bezier(0.2, 0.7, 0.2, 1) backwards;
}
.fcard-1 { width: 58%; height: 72%; top: 4%; right: 0; z-index: 3; transform: rotate(1.5deg); }
.fcard-2 { width: 46%; height: 44%; bottom: 2%; left: 4%; z-index: 2; transform: rotate(-2.5deg); animation-delay: 0.1s; }
.fcard-3 { width: 34%; height: 36%; top: 0; left: 0; z-index: 1; transform: rotate(4deg); opacity: 0.92; animation-delay: 0.2s; }
/* the back card is partly covered — keep only its title legible */
.fcard-3 .fcard-cap em { display: none; }
.fcard:hover { transform: translateY(-8px) rotate(0deg) scale(1.02); z-index: 4; }
@keyframes fcardIn {
  from { opacity: 0; transform: translateY(28px) scale(0.95); }
}
.fcard-veil {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgb(0 0 0 / 0.82), transparent 58%);
}
.fcard-cap {
  position: absolute;
  left: 1rem;
  right: 1rem;
  bottom: 0.9rem;
  text-align: left;
  color: rgb(255 255 255 / 0.82);
}
.fcard-cap i { display: block; font-style: normal; font-size: 9.5px; letter-spacing: 0.18em; color: rgb(255 255 255 / 0.6); }
.fcard-cap b { display: block; font-size: 14px; margin-top: 0.2rem; color: #fff; }
.fcard-cap em {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-style: normal;
  font-size: 11.5px;
  line-height: 1.5;
  margin-top: 0.25rem;
}

/* ---------- capability cards ---------- */
.cap-card {
  position: relative;
  overflow: hidden;
  text-align: left;
  cursor: pointer;
  border: 2.5px solid var(--ink);
  padding: 1.35rem;
  border-radius: 1rem;
  background: var(--surface);
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: transform 0.18s ease, box-shadow 0.18s ease;
}
.cap-card:hover {
  transform: translate(-3px, -3px);
  box-shadow: 9px 9px 0 var(--m-orange);
}
html.dark .cap-card { background: rgb(255 255 255 / 0.05); }
.cap-arrow { position: absolute; top: 1.1rem; right: 1.1rem; color: var(--fg-3); transition: color 0.15s ease; }
.cap-card:hover .cap-arrow { color: var(--m-orange); }
.cap-icon {
  width: 2.6rem;
  height: 2.6rem;
  border-radius: 0.7rem;
  border: 2px solid var(--ink);
  display: grid;
  place-items: center;
  margin-bottom: 0.9rem;
}
.cap-icon svg { width: 1.25rem; height: 1.25rem; }
.cap-meta {
  font-style: normal;
  font-size: 10px;
  font-weight: 700;
  color: var(--fg-2);
  padding: 0.18rem 0.5rem;
  border-radius: 0.4rem;
  border: 1.5px solid var(--ink-soft);
  background: var(--surface-2);
}

/* ---------- bento tiles (kind=bento) ---------- */
.tile {
  position: relative;
  overflow: hidden;
  border: 2.5px solid var(--ink);
  padding: 0;
  cursor: pointer;
  border-radius: 1rem;
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}
.tile:hover { transform: translate(-2px, -2px); box-shadow: 8px 8px 0 var(--m-blue); }
.tile-veil {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgb(0 0 0 / 0.82) 0%, rgb(0 0 0 / 0.28) 45%, transparent 72%);
}
.tile-cap { position: absolute; left: 1rem; right: 1rem; bottom: 0.9rem; text-align: left; color: #fff; }
.tile-cap i { display: block; font-style: normal; font-size: 9.5px; letter-spacing: 0.2em; color: rgb(255 255 255 / 0.66); }
.tile-cap b { display: block; font-size: 15px; margin-top: 0.2rem; }
.tile-cap em {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-style: normal;
  font-size: 11.5px;
  line-height: 1.55;
  margin-top: 0.3rem;
  color: rgb(255 255 255 / 0.72);
}

/* ---------- works (kind=work) ---------- */
.work {
  position: relative;
  display: block;
  width: 100%;
  overflow: hidden;
  border: 2.5px solid var(--ink);
  padding: 0;
  cursor: pointer;
  border-radius: 1rem;
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}
.work:hover { transform: translate(-2px, -2px); box-shadow: 8px 8px 0 var(--m-pink); }
.work-img { display: block; width: 100%; aspect-ratio: 4 / 5; transition: transform 0.5s ease; }
.work:hover .work-img { transform: scale(1.04); }
.work-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  align-items: flex-start;
  text-align: left;
  padding: 1rem;
  background: linear-gradient(to top, rgb(0 0 0 / 0.86) 0%, rgb(0 0 0 / 0.3) 50%, transparent 78%);
  opacity: 0;
  transition: opacity 0.25s ease;
}
.work:hover .work-overlay, .work:focus-visible .work-overlay { opacity: 1; }
/* touch devices have no hover — keep the caption visible there */
@media (hover: none) { .work-overlay { opacity: 1; } }
.work-overlay b { color: #fff; font-size: 14px; }
.work-overlay em {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-style: normal;
  font-size: 11.5px;
  line-height: 1.6;
  margin-top: 0.3rem;
  color: rgb(255 255 255 / 0.8);
}
.work-reuse {
  margin-top: 0.7rem;
  font-style: normal;
  font-size: 12px;
  font-weight: 800;
  color: rgb(11 13 19);
  background: var(--m-yellow);
  border: 2px solid #111;
  border-radius: 0.5rem;
  padding: 0.4rem 0.8rem;
}

/* ---------- model row ---------- */
.model-row::-webkit-scrollbar { height: 6px; }
.model-row::-webkit-scrollbar-thumb { background: var(--hairline); border-radius: 3px; }
.model-card {
  position: relative;
  flex: none;
  width: 14.5rem;
  overflow: hidden;
  padding: 1.25rem;
  border-radius: 1rem;
  border: 2.5px solid var(--ink);
  background: var(--surface);
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: box-shadow 0.18s ease, transform 0.18s ease;
}
.model-card:hover { transform: translate(-2px, -2px); box-shadow: 8px 8px 0 var(--m-mint); }
html.dark .model-card { background: rgb(255 255 255 / 0.05); }
.model-glow {
  position: absolute;
  top: -2.5rem;
  right: -2.5rem;
  width: 8rem;
  height: 8rem;
  border-radius: 50%;
  filter: blur(40px);
  opacity: 0.3;
}
.model-tag {
  font-size: 10.5px;
  font-weight: 800;
  padding: 0.18rem 0.55rem;
  border-radius: 0.4rem;
  border: 1.5px solid var(--ink);
}
.model-tag.is-image { background: var(--m-mint); color: #065f46; }
.model-tag.is-video { background: var(--m-blue); color: #075985; }

/* ---------- hero band + memphis deco ---------- */
.hero-band {
  border-radius: 2rem;
}
/* Floating crayon chips scattered around the headline — each is a small
   ink-outlined tile with a hard shadow and its own idle animation. */
.deco {
  position: absolute;
  display: grid;
  place-items: center;
  border: 3px solid var(--ink);
  box-shadow: 5px 5px 0 var(--shadow-ink);
  font-weight: 900;
  color: #111;
}
.deco-a {
  left: 7%; top: 22%;
  width: 3.6rem; height: 3.6rem;
  border-radius: 0.9rem;
  background: var(--m-yellow);
  font-size: 1.5rem;
  transform: rotate(-8deg);
  animation: memphis-float 6s ease-in-out infinite;
}
.deco-b {
  right: 8%; top: 16%;
  width: 3rem; height: 3rem;
  border-radius: 999px;
  background: var(--m-pink);
  font-size: 1.2rem;
  transform: rotate(6deg);
  animation: memphis-wiggle 3.5s ease-in-out infinite;
}
.deco-c {
  left: 13%; bottom: 16%;
  width: 3.2rem; height: 2.4rem;
  border-radius: 0.6rem;
  background: var(--m-blue);
  font-size: 0.95rem;
  letter-spacing: 0.05em;
  transform: rotate(9deg);
  animation: memphis-float 7s ease-in-out infinite reverse;
}
.deco-d {
  right: 13%; bottom: 20%;
  width: 3.4rem; height: 2.6rem;
  border-radius: 0.7rem;
  background: var(--m-mint);
  font-size: 1.15rem;
  transform: rotate(-7deg);
  animation: memphis-bounce 4s ease-in-out infinite;
}
.deco-star {
  left: 22%; top: 10%;
  border: none;
  box-shadow: none;
  font-size: 1.7rem;
  color: var(--m-orange);
  animation: memphis-wiggle 4s ease-in-out infinite;
}
.deco-star-2 {
  left: auto; right: 22%; top: auto; bottom: 10%;
  font-size: 1.3rem;
  color: var(--m-pink);
  animation: memphis-wiggle 5s ease-in-out infinite reverse;
}
@media (prefers-reduced-motion: reduce) { .deco { animation: none; } }

/* KPI tiles — flat crayon fills, dark ink type (readable in both themes). */
.kpi-card {
  border: 2.5px solid var(--ink);
  border-radius: 1rem;
  box-shadow: 5px 5px 0 var(--shadow-ink);
  transition: transform 0.18s ease, box-shadow 0.18s ease;
}
.kpi-card:hover { transform: translate(-2px, -2px); box-shadow: 8px 8px 0 var(--shadow-ink); }

/* ---------- gallery marquee ---------- */
.marquee-mask {
  -webkit-mask-image: linear-gradient(to right, transparent, #000 6%, #000 94%, transparent);
  mask-image: linear-gradient(to right, transparent, #000 6%, #000 94%, transparent);
}
.marquee {
  animation: marqueeScroll 42s linear infinite;
}
.marquee:hover { animation-play-state: paused; }
@keyframes marqueeScroll {
  from { transform: translateX(0); }
  to { transform: translateX(-50%); }
}
.gcard {
  position: relative;
  flex: none;
  width: 15rem;
  height: 10rem;
  overflow: hidden;
  border: 2.5px solid var(--ink);
  padding: 0;
  cursor: pointer;
  border-radius: 1rem;
  background-size: cover;
  background-position: center;
  box-shadow: 4px 4px 0 var(--shadow-ink);
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}
.gcard:hover {
  transform: translate(-2px, -2px);
  box-shadow: 7px 7px 0 var(--m-orange);
}
@media (prefers-reduced-motion: reduce) {
  .marquee { animation: none; }
}
</style>
