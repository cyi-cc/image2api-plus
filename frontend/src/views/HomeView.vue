<template>
  <div class="home">

    <!-- Hero：左文右图，右侧为 AI 作品拼贴 -->
    <section class="hero">
      <div class="hero-bg"></div>

      <div class="hero-wrap">
        <div class="hero-copy">
          <span class="badge">
            <i class="badge-dot"></i>
            {{ onlineModels.length }} 个模型在线 · {{ hasVideo ? '图像与视频' : '图像与文本' }}一站创作
          </span>

          <h1 class="title">让想象<br /><span class="outline">即刻成画</span></h1>

          <p class="subtitle">输入一句话，或拖入一张图，生成你的专属视觉作品</p>

          <div class="composer">
            <Icon name="spark" :size="18" class="composer-icon" />
            <input
              ref="inputRef"
              v-model="prompt"
              type="text"
              placeholder="描述你想要的画面，如：黄昏麦田里奔跑的金毛…"
              @keydown.enter="go"
            />
            <button class="composer-go" @click="go" aria-label="生成">
              生成
              <Icon name="arrow" :size="15" />
            </button>
          </div>

          <div class="tips">
            <span class="tips-label">试试</span>
            <button v-for="t in tips" :key="t" class="tip" @click="prompt = t; go()">{{ t }}</button>
          </div>

          <div class="hero-stats">
            <div class="hstat">
              <strong>{{ onlineModels.length }}</strong>
              <span>在线模型</span>
            </div>
            <div class="hstat">
              <strong>{{ imageQualities.length }}</strong>
              <span>图像档位</span>
            </div>
            <div class="hstat">
              <strong>/v1</strong>
              <span>开放 API</span>
            </div>
          </div>
        </div>

        <!-- Hero 拼贴：展示后台轮播位（最多 4 张，点击带入提示词去创作）；
             数据未到时先渲染同比例 shimmer 占位卡，保证首帧行高稳定、文字不跳 -->
        <div v-if="heroSlots.length" class="hero-art" :class="{ linked: heroArts.length }">
          <figure
            v-for="(a, i) in heroSlots"
            :key="a.id"
            class="art"
            :class="[a.ph ? 'ph' : '', 'a' + (i + 1)]"
            role="button"
            tabindex="0"
            :title="a.prompt ? `用「${a.title}」的提示词去创作` : a.title"
            @click="!a.ph && goStudio(a.prompt)"
            @keydown.enter="!a.ph && goStudio(a.prompt)"
          >
            <img v-if="!a.ph" :src="imgSrc(a.image)" :alt="a.title" />
            <figcaption v-if="a.title">{{ a.title }}</figcaption>
          </figure>
        </div>
      </div>
    </section>

    <!-- 能力矩阵 -->
    <section class="features reveal">
      <div class="section-head">
        <div>
          <span class="eyebrow">01 · 能力</span>
          <h2>一站式创作引擎</h2>
        </div>
      </div>
      <div class="feature-grid">
        <div v-for="f in features" :key="f.title" class="feature">
          <span class="f-icon"><Icon :name="f.icon" :size="18" /></span>
          <h3>{{ f.title }}</h3>
          <p>{{ f.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 精选作品 -->
    <section class="gallery reveal">
      <div class="section-head">
        <div>
          <span class="eyebrow">02 · 灵感</span>
          <h2>看看别人在画什么</h2>
        </div>
        <router-link to="/user" class="more">
          去创作
          <Icon name="arrow" :size="13" />
        </router-link>
      </div>
      <div v-if="inspireItems.length" class="work-grid">
        <figure
          v-for="(w, i) in inspireItems"
          :key="w.id"
          class="work-card"
          role="button"
          tabindex="0"
          @click="goStudio(w.prompt)"
          @keydown.enter="goStudio(w.prompt)"
        >
          <img :src="imgSrc(w.image)" :alt="w.title" loading="lazy" />
          <span class="work-idx">{{ String(i + 1).padStart(2, '0') }}</span>
          <figcaption>
            <span class="work-name">{{ w.title }}</span>
            <span class="work-model">{{ w.prompt ? w.prompt : '点击用这张的提示词创作' }}</span>
          </figcaption>
        </figure>
        <router-link to="/user" class="work-cta">
          <Icon name="spark" :size="22" />
          <span>创作你的第一张</span>
          <Icon name="arrow" :size="14" />
        </router-link>
      </div>
      <p v-else class="gallery-empty">暂无灵感作品</p>
    </section>

    <!-- CTA -->
    <section class="cta reveal">
      <h2>准备好开始创作了吗？</h2>
      <p>注册账号，即刻体验 AI 图像、视频与文字创作</p>
      <router-link to="/user" class="cta-btn">
        立即开始
        <Icon name="arrow" :size="15" />
      </router-link>
    </section>

    <footer class="foot">
      <div class="foot-inner">
        <div class="f-grid">
          <div class="f-brand">
            <div class="f-logo">
              <img v-if="site.logo" :src="site.logo" :alt="site.title" class="f-logo-img" />
              <Logo v-else :size="30" />
              <span class="f-word brand-word">{{ site.title }}</span>
            </div>
            <p class="f-desc">按需计费的 AI 视觉创作平台——{{ onlineModels.length }} 个模型在线，按真实模型档位计费，只为实际生成的内容付费。</p>
            <div class="f-social">
              <a v-if="site.contact?.qq_link" :href="site.contact.qq_link" target="_blank" rel="noopener" class="fs-item" title="客服 QQ"><Icon name="chat" :size="15" /></a>
              <button v-if="site.contact?.qq_group" type="button" class="fs-item" title="点击复制 QQ 群号" @click="copyQqGroup"><Icon name="team" :size="15" /></button>
              <a v-if="site.contact?.email" :href="`mailto:${site.contact.email}`" class="fs-item" :title="site.contact.email"><Icon name="mail" :size="15" /></a>
              <a v-if="site.contact?.shop" :href="site.contact.shop" target="_blank" rel="noopener" class="fs-item" title="小铺"><Icon name="wallet" :size="15" /></a>
            </div>
          </div>
          <nav class="f-col">
            <span class="f-col-h">产品</span>
            <router-link to="/user" class="f-link">创作</router-link>
            <router-link to="/canvas" class="f-link">无限画布</router-link>
            <router-link to="/logs" class="f-link">我的作品</router-link>
          </nav>
          <nav class="f-col">
            <span class="f-col-h">开发者</span>
            <router-link to="/docs" class="f-link">API 文档</router-link>
            <router-link to="/mylogs" class="f-link">调用日志</router-link>
          </nav>
          <nav class="f-col">
            <span class="f-col-h">支持</span>
            <router-link to="/about" class="f-link">关于我们</router-link>
            <router-link to="/orders" class="f-link">我的订单</router-link>
          </nav>
        </div>
        <div class="f-bottom">
          <span>© {{ new Date().getFullYear() }} {{ site.title }} · 保留所有权利</span>
          <span class="f-slogan">让想象即刻成画</span>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import Logo from '../components/Logo.vue'
import { api, generatedUrl } from '../api'
import { site } from '../site'
import { isAuthed, openLogin } from '../auth'

const router = useRouter()
const prompt = ref('')
const inputRef = ref(null)
const onlineModels = ref([])
const hasVideo = computed(() => onlineModels.value.some(model => model.type === 'video'))
const imageModels = computed(() => onlineModels.value.filter(model => model.type === 'image'))
const textModels = computed(() => onlineModels.value.filter(model => model.type === 'text'))
const imageQualities = computed(() => [...new Set(imageModels.value.flatMap(model => model.resolutions || []))])
const maxRefImages = computed(() => Math.max(0, ...imageModels.value.map(model => Number(model.max_reference_images || 0))))

// 展示位：轮播类型渲染为 Hero 拼贴（最多 4 格，点击带提示词去创作）+ 灵感类型渲染为作品墙
const carouselItems = ref([])
const carouselLoading = ref(true) // 首屏先渲染拼贴占位，行高稳定，文字不跳
const PH_SLOTS = [
  { id: 'ph-1', ph: true }, { id: 'ph-2', ph: true },
  { id: 'ph-3', ph: true }, { id: 'ph-4', ph: true }
]
const heroSlots = computed(() => (carouselLoading.value && !carouselItems.value.length) ? PH_SLOTS : carouselItems.value.slice(0, 4))
const inspireItems = ref([])
// 后台未配置展示位时的内置兜底图（/public/showcase 下的本地渐变图）
const FALLBACK_HERO = [
  { id: 'fb-h1', title: '霓虹都市', prompt: '雨夜霓虹街道，赛博朋克光影', image: '/showcase/hero-1.svg' },
  { id: 'fb-h2', title: '深海蓝调', prompt: '深海蓝调梦境，光斑浮动', image: '/showcase/hero-2.svg' },
  { id: 'fb-h3', title: '熔金落日', prompt: '熔金色落日余晖', image: '/showcase/hero-3.svg' },
  { id: 'fb-h4', title: '翡翠星野', prompt: '翡翠色星野与极光', image: '/showcase/hero-4.svg' },
]
const FALLBACK_WORKS = [1, 2, 3, 4, 5, 6, 7].map(i => ({
  id: 'fb-w' + i, title: '灵感作品 ' + i, prompt: '', image: `/showcase/work-${i}.svg`,
}))
// Hero 拼贴固定 4 格；后端已限制轮播最多 4 张，此处 slice 兜底
const heroArts = computed(() => carouselItems.value.slice(0, 4))

// 区块入场动画：滚动到视口时渐显一次，之后不再观察
let revealObs = null

onMounted(async () => {
  try {
    const [m, sc] = await Promise.all([
      api('/managed-models'),
      api('/showcase'),
    ])
    onlineModels.value = (m.data?.data || []).filter(model => model.sellable)
    const data = sc.data?.data || {}
    // hero 拼贴按 hero → 作品墙兜底取图，保证首屏有图可看
    const pool = [...(data.hero || []), ...(data.work || [])]
    carouselItems.value = pool.filter(i => i.image).slice(0, 4)
    if (!carouselItems.value.length) carouselItems.value = FALLBACK_HERO
    inspireItems.value = (data.work || []).length ? data.work : FALLBACK_WORKS
    carouselLoading.value = false
  } catch {
    carouselItems.value = FALLBACK_HERO
    inspireItems.value = FALLBACK_WORKS
    carouselLoading.value = false /* 展示位加载失败不阻塞首页 */
  }
  await nextTick()
  if (typeof IntersectionObserver === 'undefined') return
  revealObs = new IntersectionObserver((entries) => {
    for (const en of entries) {
      if (en.isIntersecting) {
        en.target.classList.add('in')
        revealObs.unobserve(en.target)
      }
    }
  }, { threshold: 0.12 })
  document.querySelectorAll('.home .reveal').forEach(el => revealObs.observe(el))
})

onBeforeUnmount(() => revealObs?.disconnect())

// 统一的"去创作"入口：带提示词跳转到画图台；未登录弹登录框
function goStudio(withPrompt = '') {
  const text = String(withPrompt || '').trim()
  const target = text ? { path: '/user', query: { prompt: text } } : { path: '/user' }
  if (isAuthed()) { router.push(target); return }
  openLogin(router.resolve(target).fullPath)
}

function go() {
  goStudio(prompt.value)
}

const tips = ['雨夜东京小巷', '雪山极光', '和服少女', '水墨神龙', '深海幻境']

const features = computed(() => {
  const items = [{
    icon: 'image', title: '图像生成',
    desc: `${imageModels.value.length} 个图像模型，支持 ${imageQualities.value.join(' / ') || '已配置'} 档位`
  }]
  if (hasVideo.value) {
    items.push({ icon: 'video', title: '视频生成', desc: '按模型配置支持视频时长与参考素材控制' })
  }
  if (textModels.value.length) {
    items.push({ icon: 'chat', title: '文字生成', desc: `${textModels.value.length} 个文本模型，按次计费` })
  }
  items.push({
    icon: 'layers', title: '参考素材创作',
    desc: maxRefImages.value > 0 ? `当前图像模型最多支持 ${maxRefImages.value} 张参考图` : '按模型能力支持参考素材'
  })
  items.push({ icon: 'key', title: '开放 API', desc: '/v1/* 接口支持 Key 鉴权、流式结果与失败自动退款' })
  return items
})

// Resolve an image reference: external URLs pass through, relative paths
// (like "user/abc.png") are served from /images by the backend.
function imgSrc(image) {
  if (!image) return ''
  if (/^https?:\/\//i.test(image)) return image
  if (image.startsWith('/')) return image // 内置静态资源（public/ 下）
  return generatedUrl(image)
}

async function copyQqGroup() {
  const g = (site.contact?.qq_group || '').trim()
  if (!g) return
  try {
    await navigator.clipboard.writeText(g)
  } catch { /* clipboard 不可用时静默 */ }
}
</script>

<style scoped>
/* vivid 旧版色板（原版 :root 值，局部覆盖使页面与 vivid 完全一致） */
.home {
  --fg: #0f172a;
  --fg-2: #3f4b5f;
  --fg-3: #475569;
  --fg-faint: #5a6a7d;
  --surface: #ffffff;
  --surface-solid: #ffffff;
  --surface-2: #f4f4f6;
  --surface-3: #e9e9ec;
  --hairline: rgba(15, 23, 42, 0.08);
  --hover: rgba(15, 23, 42, 0.05);
  --accent: #18181b;
  --accent-strong: #18181b;
  --primary-soft: rgba(24, 24, 27, 0.06);
  --grad: linear-gradient(135deg, #27272a 0%, #09090b 100%);
  --shadow-sm: 0 1px 2px rgba(15, 23, 42, 0.04), 0 1px 3px rgba(15, 23, 42, 0.05);
  --shadow-glow: 0 10px 24px -10px rgba(9, 9, 11, 0.5);
  --ease: cubic-bezier(0.25, 0.1, 0.25, 1);
  --font-display: ui-sans-serif, system-ui, sans-serif;
  position: relative;
  min-height: 100%;
  display: flex;
  flex-direction: column;
}


/* Hero：左文右图 */
.hero {
  position: relative;
  width: 100%;
  margin: 0 auto;
  padding: 118px 20px 64px;
}
.hero-bg {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: radial-gradient(900px 520px at 70% 10%, rgba(244, 114, 52, 0.07), transparent 70%);
}
.hero-wrap {
  position: relative;
  width: min(1400px, 100%);
  margin: 0 auto;
  display: grid;
  grid-template-columns: 1.02fr 0.98fr;
  gap: 56px;
  align-items: center;
}
.hero-copy {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  text-align: left;
}

/* 右侧作品拼贴 */
.hero-art {
  position: relative;
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 18px;
  animation: fadeUp 0.7s var(--ease) 0.25s both;
}
.hero-art::before {
  content: '';
  position: absolute;
  inset: 8% -6% 6% -4%;
  border-radius: 50%;
  background: radial-gradient(closest-side, rgba(24, 24, 27, 0.16), transparent);
  filter: blur(30px);
  z-index: -1;
}
.art {
  position: relative;
  margin: 0;
  border-radius: 22px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.65);
  box-shadow: 0 24px 56px -22px rgba(30, 20, 90, 0.38);
  transition: transform 0.35s var(--ease), box-shadow 0.35s ease;
}
html.dark .art { border-color: var(--ink-soft); }
.art img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.art.a1 { aspect-ratio: 3 / 4; transform: translateY(22px) rotate(-1.6deg); }
.art.a2 { aspect-ratio: 3 / 4; transform: translateY(-12px) rotate(1.4deg); }
.art.a3 { aspect-ratio: 1 / 1; transform: translateY(14px) rotate(1.6deg); }
.art.a4 { aspect-ratio: 3 / 4; margin-top: -34px; transform: translateY(-6px) rotate(-1.4deg); }
/* 数据未到时的 shimmer 占位卡：同尺寸占住拼贴区域 */
.art.ph {
  position: relative;
  border-style: solid;
  border-color: transparent;
  background: var(--surface-2);
  animation: fadeUp 0.7s var(--ease) 0.25s both;
  box-shadow: none;
  cursor: default;
  overflow: hidden;
}
.art.ph::after {
  content: "";
  position: absolute;
  inset: 0;
  border-radius: inherit;
  background: linear-gradient(100deg, rgba(242, 241, 247, 0) 30%, rgba(234, 230, 251, 0.95) 44%, rgba(231, 236, 252, 0.95) 52%, rgba(242, 241, 247, 0) 68%);
  transform: translateX(-100%);
  animation: imgSweep 1.6s ease-in-out infinite;
  will-change: transform;
  pointer-events: none;
}
@keyframes imgSweep { to { transform: translateX(100%); } }
/* 拼贴卡缓慢悬浮（用独立的 translate 属性，不与 transform 冲突） */
.art:not(.ph) { animation: bob 6s ease-in-out infinite; }
.art.a2:not(.ph) { animation-delay: -1.5s; }
.art.a3:not(.ph) { animation-delay: -3s; }
.art.a4:not(.ph) { animation-delay: -4.5s; }
@keyframes bob {
  0%, 100% { translate: 0 0; }
  50% { translate: 0 -8px; }
}
.art:hover {
  transform: translateY(-4px) rotate(0deg) scale(1.02);
  box-shadow: 0 30px 64px -20px rgba(30, 20, 90, 0.45);
}
/* 轮播位图片：可点击，悬停显示标题浮层 */
.hero-art.linked .art {
  cursor: pointer;
}
.hero-art.linked .art figcaption {
  position: absolute;
  inset: auto 0 0 0;
  padding: 26px 14px 12px;
  background: linear-gradient(to top, rgba(15, 23, 42, 0.72), transparent);
  color: #fff;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.01em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  opacity: 0;
  translate: 0 6px;
  transition: opacity 0.28s ease, translate 0.28s var(--ease);
}
.hero-art.linked .art:hover figcaption,
.hero-art.linked .art:focus-visible figcaption {
  opacity: 1;
  translate: 0 0;
}

/* Hero 入场节奏 */
@keyframes fadeUp {
  from { opacity: 0; translate: 0 14px; }
  to { opacity: 1; translate: 0 0; }
}
.badge { animation: fadeUp 0.55s var(--ease) 0.05s both; }
.title { animation: fadeUp 0.55s var(--ease) 0.13s both; }
.subtitle { animation: fadeUp 0.55s var(--ease) 0.21s both; }
.composer { animation: fadeUp 0.55s var(--ease) 0.29s both; }
.tips { animation: fadeUp 0.55s var(--ease) 0.37s both; }
.hero-stats { animation: fadeUp 0.55s var(--ease) 0.45s both; }

.badge {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 16px;
  border-radius: 999px;
  background: var(--surface);
  border: 1px solid rgba(24, 24, 27, 0.22);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg-2);
  box-shadow: 0 2px 10px rgba(24, 24, 27, 0.08);
}
.badge-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #10b981;
  box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.15);
}

.title {
  position: relative;
  margin-top: 26px;
  font-size: clamp(42px, 4.8vw, 66px);
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: -0.045em;
  color: var(--fg);
}
.outline {
  color: transparent;
  background: linear-gradient(115deg, #18181b, #71717a);
  -webkit-background-clip: text;
  background-clip: text;
}

.subtitle {
  position: relative;
  margin-top: 18px;
  font-size: 16px;
  line-height: 1.7;
  color: var(--fg-2);
  max-width: 460px;
}

/* 输入框：渐变描边的"魔法输入框" */
.composer {
  position: relative;
  margin-top: 32px;
  width: 100%;
  max-width: 560px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 8px 8px 18px;
  border-radius: 999px;
  border: 1.5px solid transparent;
  background:
    linear-gradient(var(--surface-solid), var(--surface-solid)) padding-box,
    linear-gradient(#e4e4e7, #e4e4e7) border-box;
  box-shadow: 0 16px 44px -14px rgba(24, 24, 27, 0.35);
  transition: box-shadow 0.25s ease;
}
.composer:focus-within {
  box-shadow: 0 0 0 4px rgba(24, 24, 27, 0.1), 0 20px 54px -12px rgba(24, 24, 27, 0.45);
}
.composer-icon {
  width: 18px;
  height: 18px;
  color: var(--fg-3);
  flex-shrink: 0;
}
.composer input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  padding: 12px 0;
  font-size: 15px;
  font-family: inherit;
  color: var(--fg);
}
.composer input::placeholder { color: var(--fg-3); }
.composer-go {
  height: 48px;
  padding: 0 24px;
  border: none;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-family: inherit;
  font-size: 14px;
  font-weight: 700;
  color: #fff;
  background: var(--grad);
  cursor: pointer;
  flex-shrink: 0;
  box-shadow: var(--shadow-glow);
  transition: transform 0.15s ease, filter 0.2s ease;
}
.composer-go:hover {
  transform: scale(1.05);
  filter: brightness(1.1);
}
.composer-go svg { width: 15px; height: 15px; }

.tips {
  position: relative;
  margin-top: 16px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-start;
  gap: 8px;
}
.tips-label {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg-3);
}
.tip {
  padding: 6px 14px;
  border: 1px solid var(--surface-3);
  border-radius: 999px;
  background: var(--surface);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg-2);
  cursor: pointer;
  font-family: inherit;
  transition: border-color 0.15s ease, color 0.15s ease, transform 0.15s ease;
}
.tip:hover {
  color: var(--accent);
  border-color: rgba(24, 24, 27, 0.45);
  background: rgba(24, 24, 27, 0.04);
  transform: translateY(-1px);
}

/* Hero 统计 */
.hero-stats {
  position: relative;
  margin-top: 40px;
  display: flex;
  align-items: center;
  gap: 40px;
}
.hstat {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.hstat strong {
  font-size: 24px;
  font-weight: 800;
  letter-spacing: -0.03em;
  color: var(--fg);
  font-variant-numeric: tabular-nums;
}
.hstat span {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg-3);
}
.hstat + .hstat {
  padding-left: 40px;
  border-left: 1px solid var(--surface-3);
}

/* 区块入场：滚动渐显 */
.reveal {
  opacity: 0;
  translate: 0 26px;
  transition: opacity 0.7s var(--ease), translate 0.7s var(--ease);
}
.reveal.in {
  opacity: 1;
  translate: 0 0;
}
@media (prefers-reduced-motion: reduce) {
  .reveal { opacity: 1; translate: none; transition: none; }
}

/* 能力矩阵 */
.features {
  width: min(1400px, 100%);
  margin: 0 auto;
  padding: 72px 20px 24px;
}
.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.16em;
  color: var(--accent);
  text-transform: uppercase;
  margin-bottom: 8px;
}
.eyebrow::before {
  content: '';
  width: 22px;
  height: 2px;
  border-radius: 2px;
  background: var(--grad);
}
.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  margin-bottom: 20px;
}
.section-head h2 {
  font-size: 28px;
  font-weight: 800;
  letter-spacing: -0.03em;
  color: var(--fg);
}
/* Bento 网格：3 列，首尾卡片跨两列 */
.feature-grid {
  margin-top: 28px;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
}
.feature {
  padding: 28px 26px;
  border-radius: 22px;
  background: var(--surface);
  border: 1px solid var(--surface-3);
  transition: transform 0.25s var(--ease), box-shadow 0.25s var(--ease), border-color 0.25s ease;
}
.feature:nth-child(1) {
  grid-column: span 2;
  position: relative;
  overflow: hidden;
  background: linear-gradient(135deg, rgba(24, 24, 27, 0.07), rgba(99, 102, 241, 0.03) 60%, var(--surface));
}
.feature:nth-child(4):last-child { grid-column: span 2; }
.feature:nth-child(3):last-child { grid-column: span 3; }
.feature:nth-child(5) {
  background: var(--surface);
}
.feature:hover {
  transform: translateY(-4px);
  border-color: rgba(24, 24, 27, 0.35);
  box-shadow: 0 18px 40px rgba(24, 24, 27, 0.12);
}
.f-icon :deep(svg), .f-icon svg { color: #18181b; }
.f-icon {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: rgba(24, 24, 27, 0.12);
  color: #18181b;
}
.feature h3 {
  margin-top: 16px;
  font-size: 15.5px;
  font-weight: 700;
  color: var(--fg);
}
.feature p {
  margin-top: 7px;
  font-size: 13px;
  line-height: 1.65;
  color: var(--fg-2);
}

/* 精选作品 */
.gallery {
  width: min(1400px, 100%);
  margin: 0 auto;
  padding: 48px 20px 8px;
}
.more {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-2);
  text-decoration: none;
  transition: color 0.15s ease;
}
.more:hover { color: var(--accent); }
.work-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}
.work-card {
  position: relative;
  margin: 0;
  aspect-ratio: 3 / 2;
  border-radius: 18px;
  overflow: hidden;
  cursor: pointer;
  border: 1px solid var(--hairline);
  box-shadow: var(--shadow-sm);
  transition: transform 0.25s var(--ease), box-shadow 0.25s var(--ease);
}
.work-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 18px 40px rgba(24, 24, 27, 0.16);
}
.work-card img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.5s ease;
}
.work-card:hover img {
  transform: scale(1.05);
}
.work-idx {
  position: absolute;
  top: 12px;
  left: 14px;
  font-family: 'SF Mono', Consolas, monospace;
  font-size: 12px;
  font-weight: 700;
  color: #fff;
  background: rgba(0, 0, 0, 0.4);
  -webkit-backdrop-filter: blur(6px);
  backdrop-filter: blur(6px);
  padding: 3px 10px;
  border-radius: 999px;
}
/* 灵感区尾部的创作引导卡 */
.work-cta {
  aspect-ratio: 3 / 2;
  border-radius: 18px;
  border: 1.5px dashed rgba(24, 24, 27, 0.35);
  background: rgba(24, 24, 27, 0.04);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--accent);
  font-size: 13.5px;
  font-weight: 700;
  text-decoration: none;
  transition: background 0.2s ease, border-color 0.2s ease, transform 0.2s var(--ease);
}
.work-cta:hover {
  background: rgba(24, 24, 27, 0.08);
  border-color: rgba(24, 24, 27, 0.55);
  border-style: solid;
  transform: translateY(-3px);
}
.work-cta svg:first-child { width: 22px; height: 22px; }

.work-card figcaption {
  position: absolute;
  inset: auto 0 0 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 12px 14px;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.6), transparent);
  opacity: 0;
  transition: opacity 0.25s ease;
}
.work-card:hover figcaption {
  opacity: 1;
}
.work-name {
  font-size: 13.5px;
  font-weight: 700;
  color: #fff;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.work-model {
  font-size: 11px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.75);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.gallery-empty {
  padding: 40px 0;
  text-align: center;
  font-size: 13.5px;
  color: var(--fg-3);
}

/* CTA */
.cta {
  width: min(1400px, calc(100% - 40px));
  margin: 40px auto 0;
  padding: 72px 32px;
  border-radius: 28px;
  background: linear-gradient(135deg, #27272a 0%, #09090b 100%);
  text-align: center;
  position: relative;
  overflow: hidden;
}
.cta::before {
  content: '';
  position: absolute;
  width: 560px;
  height: 560px;
  left: 50%;
  top: -320px;
  transform: translateX(-50%);
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.2), transparent 70%);
  pointer-events: none;
}
.cta::after {
  content: '';
  position: absolute;
  width: 420px;
  height: 420px;
  right: -140px;
  bottom: -260px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.12), transparent 70%);
  pointer-events: none;
}
.cta h2 {
  position: relative;
  font-size: 32px;
  font-weight: 800;
  letter-spacing: -0.03em;
  color: #fff;
}
.cta p {
  position: relative;
  margin-top: 12px;
  font-size: 14.5px;
  color: rgba(255, 255, 255, 0.82);
}
.cta-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-top: 30px;
  padding: 14px 34px;
  border-radius: 999px;
  font-size: 14.5px;
  font-weight: 700;
  color: #18181b;
  background: #fff;
  text-decoration: none;
  box-shadow: 0 14px 34px -10px rgba(30, 20, 90, 0.45);
  transition: transform 0.15s ease, filter 0.2s ease;
}
.cta-btn:hover {
  transform: translateY(-2px);
  filter: brightness(0.96);
}

/* 页脚 */
.foot {
  position: relative;
  margin-top: 96px;
  background: transparent;
  color: var(--fg);
}
.foot::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: linear-gradient(to right, transparent, rgba(15, 23, 42, 0.14) 30%, rgba(15, 23, 42, 0.14) 70%, transparent);
}
.foot-inner {
  max-width: 1400px;
  margin: 0 auto;
  padding: 0 32px;
}
.f-grid {
  display: grid;
  grid-template-columns: 1.6fr 1fr 1fr 1fr;
  gap: 48px;
  padding: 56px 0 48px;
}
.f-brand {
  max-width: 340px;
}
.f-logo {
  display: flex;
  align-items: center;
  gap: 10px;
}
.f-logo-img { width: 30px; height: 30px; object-fit: contain; }
.f-word {
  font-family: var(--font-display);
  font-size: 18px;
  font-weight: 800;
  letter-spacing: -0.02em;
  color: var(--fg);
}
.f-desc {
  color: var(--fg-3);
  margin-top: 14px;
  font-size: 13px;
  line-height: 1.8;
}
.f-social {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 16px;
}
.fs-item {
  color: var(--fg-2);
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: var(--surface-2);
  border: 1px solid var(--surface-3);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  text-decoration: none;
  transition: background 0.2s ease, border-color 0.2s ease, color 0.2s ease, transform 0.2s var(--ease), box-shadow 0.2s ease;
}
.fs-item:hover {
  color: #fff;
  background: var(--accent);
  border-color: var(--accent);
  transform: translateY(-2px);
  box-shadow: 0 8px 18px -6px rgba(24, 24, 27, 0.45);
}
.f-col {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.f-col-h {
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-faint);
  margin-bottom: 4px;
  font-size: 12px;
  font-weight: 700;
}
.f-link {
  position: relative;
  color: var(--fg-2);
  width: fit-content;
  font-size: 13.5px;
  text-decoration: none;
  transition: color 0.2s ease;
}
.f-link::after {
  content: '';
  position: absolute;
  left: 0;
  bottom: -3px;
  width: 100%;
  height: 1px;
  background: var(--accent);
  transform: scaleX(0);
  transform-origin: left;
  transition: transform 0.25s var(--ease);
}
.f-link:hover {
  color: var(--accent);
}
.f-link:hover::after {
  transform: scaleX(1);
}
.f-bottom {
  display: flex;
  justify-content: space-between;
  padding-top: 28px;
  padding-bottom: 36px;
  border-top: 1px solid var(--hairline);
  color: var(--fg-faint);
  font-size: 13px;
}
.f-slogan {
  color: var(--fg-3);
}

@media (max-width: 900px) {
  .hero { padding: 92px 18px 44px; }
  .hero-wrap { grid-template-columns: 1fr; gap: 44px; }
  .hero-copy { align-items: center; text-align: center; }
  .tips { justify-content: center; }
  .hero-art { gap: 12px; }
  .art.a1, .art.a2, .art.a3 { transform: none; }
  .art.a4 { margin-top: 0; transform: none; }
  .feature-grid { grid-template-columns: repeat(2, 1fr); }
  .feature:nth-child(1) { grid-column: span 2; }
  .work-grid { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 520px) {
  .title { font-size: 44px; }
  .composer { padding: 6px 6px 6px 14px; }
  .composer-go { height: 42px; padding: 0 16px; }
  .feature:nth-child(1) { grid-column: auto; }
  .hero-stats { flex-direction: row; gap: 16px; margin-top: 40px; }
  .hstat strong { font-size: 17px; }
  .hstat span { font-size: 11px; }
  .hstat + .hstat { padding-left: 16px; border-left: 1px solid var(--surface-3); }
  .feature-grid { grid-template-columns: 1fr; }
  .features { padding: 36px 18px 16px; }
  .gallery { padding: 20px 18px 8px; }
  .section-head h2 { font-size: 21px; }
  .cta { padding: 48px 20px; }
  .cta h2 { font-size: 23px; }
  .foot-inner { padding: 0 20px; }
  .f-grid { grid-template-columns: 1fr; gap: 32px; padding: 44px 0 36px; }
  .f-bottom { flex-direction: column; gap: 10px; padding-bottom: 30px; }
}
@media (max-width: 400px) {
  .work-grid { grid-template-columns: 1fr; }
}
</style>
