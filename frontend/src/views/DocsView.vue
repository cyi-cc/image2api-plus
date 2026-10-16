<script setup>
// API 对接文档 — Vivid 协议（/v1/generate NDJSON 长连接）。列出在售模型
// 与计费档位，示例代码直接可跑。
import { ref, computed, onMounted } from 'vue'
import { auth } from '../auth'
import { api } from '../api'
import Icon from '../components/Icon.vue'
import { points } from '../credits'
import { mediaCaps, presetMap } from '../videoCaps'

const base = computed(() => location.origin)            // /v1 is same-origin (dev: Vite proxy)
const keyHint = computed(() => auth.user?.api_keys?.[0]?.key_preview || 'vk_xxxx')

const models = ref([])
const presets = ref({})   // /video-presets: key → 预设（参考资产分类上限的真源）
onMounted(async () => {
  const [r, p] = await Promise.all([api('/managed-models'), api('/video-presets')])
  if (r.ok) models.value = (r.data?.data || []).filter((m) => m.sellable)
  if (p.ok) presets.value = presetMap(p.data?.data)
})

// 各类参考资产的上限(图片/视频/音频)由后端预设给出,不在这里写死。
function caps(m) { return mediaCaps(m, presets.value[m.id]) }
// 参考图张数:支持音视频参考的模型,max_reference_images 是各类资产合计,图片自己
// 的上限来自预设 max_images。
function refImages(m) { return caps(m)?.images || m.max_reference_images }

const imageModels = computed(() => models.value.filter((m) => m.type === 'image'))
const videoModels = computed(() => models.value.filter((m) => m.type === 'video'))
function pubName(m) {
  return m?.alias || m?.id || ''
}
const sampleImage = computed(() => pubName(imageModels.value[0]) || 'gpt-image-2')
const sampleVideo = computed(() => pubName(videoModels.value[0]) || 'video-model')
const sampleQuality = computed(() => (imageModels.value[0]?.resolutions || [])[0] || '1K')
const sampleVQuality = computed(() => (videoModels.value[0]?.resolutions || [])[0] || '720p')
const sampleSeconds = computed(() => String(videoModels.value[0]?.durations?.[0] || '8s').replace(/s$/, ''))

function modeEmpty(m) {
  if (m.type === 'image') return !(m.resolutions || []).length && !m.image_to_image && !(m.ratios || []).length
  return !(m.resolutions || []).length && !(m.durations || []).length && !m.max_reference_images && !caps(m) && !(m.ratios || []).length
}

// ---- 定价(同模型管理的分档展示):代理账号看代理价,某档没设代理价时回退普通价;
// 普通价为空表示该档不支持,不展示。视频价 = 分辨率价 + 时长价(按秒计价的模型给 /s 价)。
const isAgent = computed(() => auth.user?.role === 'agent')
function tierPrice(normal, agent, key) {
  const n = (normal || {})[key]
  if (n == null) return null
  if (isAgent.value) {
    const a = (agent || {})[key]
    if (a != null) return Number(a)
  }
  return Number(n)
}
function resPrice(m, r) { return tierPrice(m.prices, m.prices_agent, r) }
function durPrice(m, d) { return tierPrice(m.duration_prices, m.duration_prices_agent, d) }
function perSecondPrice(m) { return tierPrice(m.duration_prices, m.duration_prices_agent, 'per_second') }
function priceEmpty(m) {
  if ((m.resolutions || []).some((r) => resPrice(m, r) != null)) return false
  if (m.type !== 'video') return true
  if (perSecondPrice(m) != null) return false
  return !(m.durations || []).some((d) => durPrice(m, d) != null)
}
// 视频时长展示:固定/离散档列出来;区间档(duration_min/max)显示 min–max s。
function durText(m) {
  const ds = m.durations || []
  if (ds.length) return ds.join('/')
  if ((m.duration_max || 0) > 0) return `${m.duration_min}–${m.duration_max}s`
  return ''
}

// ---- request parameter tables ----
const generateParams = [
  ['model', 'string', '必填', '模型名(别名优先),见上表 /v1/models 的 id'],
  ['prompt', 'string', '必填', '文本描述'],
  ['quality', 'string', '可选', '清晰度档位,取该模型 qualities 之一(图像如 1K/2K/4K,视频如 720p/1080p);不传 = 该模型最低档'],
  ['ratio', 'string', '可选', '宽高比,如 "16:9";不传 = 类型默认'],
  ['duration', 'int64', '可选', '视频时长(秒);range/options 模型必填,图片忽略'],
  ['refs', 'array<string>', '可选', '参考素材 key 数组:先 POST /v1/uploads 拿预签名直传,PUT 上传后把返回的 key 放这里。30 分钟内有效,张数上限见模型表'],
  ['jobId', 'string', '可选', '重连模式:流意外断开后只带 jobId 原样重发,不创建新任务、不重复扣费;已完成直接回终态帧'],
]

const uploadParams = [
  ['kind', 'string', '必填', '素材大类:image / video / audio(与文件实际类型一致)'],
  ['mime', 'string', '必填', '文件 MIME(如 image/png),须与 kind 自洽;PUT 上传的 Content-Type 必须与它一字不差(已签进 URL)'],
]

// ---- examples (built in script so refs resolve correctly) ----
const examples = computed(() => [
  {
    title: '文生图 · curl (NDJSON 流)',
    code:
`curl -N ${base.value}/v1/generate \\
  -H "Authorization: Bearer ${keyHint.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${sampleImage.value}",
    "prompt": "a corgi running in a golden wheat field, cinematic",
    "quality": "${sampleQuality.value}",
    "ratio": "1:1"
  }'
# 响应是 NDJSON:首行 {"jobId":"...","status":"running"},空行为心跳(忽略),
# 末行终态 {"status":"succeeded","result":"<预签名URL>","creditsCharged":10}`,
  },
  {
    title: '生图 · Python (流式读终态)',
    code:
`import json, requests

r = requests.post("${base.value}/v1/generate", stream=True, timeout=600,
    headers={"Authorization": "Bearer ${keyHint.value}"},
    json={"model": "${sampleImage.value}", "prompt": "a cute cat", "quality": "${sampleQuality.value}"})

job_id, final = None, None
for line in r.iter_lines():
    if not line:  # 空行 = 心跳,忽略
        continue
    frame = json.loads(line)
    job_id = frame.get("jobId") or job_id
    if frame.get("status") in ("succeeded", "failed"):
        final = frame
        break

# 断流重连:只带 jobId 重发即继续等同一任务(不重复扣费)
# requests.post(..., json={"jobId": job_id})
print(final)`,
  },
  {
    title: '参考素材上传 · curl (预签名直传)',
    code:
`# 1) 申请上传会话 → 拿 {key, url}
resp=$(curl -s ${base.value}/v1/uploads \\
  -H "Authorization: Bearer ${keyHint.value}" \\
  -H "Content-Type: application/json" \\
  -d '{"kind":"image","mime":"image/png"}')

# 2) PUT 直传(Content-Type 必须与申请的 mime 完全一致)
curl -X PUT -H "Content-Type: image/png" --data-binary @input.png "<url>"

# 3) key 放进 /v1/generate 的 refs
curl -N ${base.value}/v1/generate \\
  -H "Authorization: Bearer ${keyHint.value}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${sampleImage.value}","prompt":"改成赛博朋克风格","refs":["<key>"]}'`,
  },
  {
    title: '视频 · curl',
    code:
`curl -N ${base.value}/v1/generate \\
  -H "Authorization: Bearer ${keyHint.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${sampleVideo.value}",
    "prompt": "a paper boat sailing down a rainy street, cinematic",
    "quality": "${sampleVQuality.value}",
    "ratio": "16:9",
    "duration": ${sampleSeconds.value}
  }'
# 与图像同一端点:quality 传分辨率档,duration 传秒数`,
  },
  {
    title: '列出模型 · curl',
    code:
`curl ${base.value}/v1/models \\
  -H "Authorization: Bearer ${keyHint.value}"

# => [{"id":"...","kind":"image","qualities":["1K","2K"],"refImages":4}, ...]`,
  },
  {
    title: '查询余额 · curl',
    code:
`curl ${base.value}/v1/balance \\
  -H "Authorization: Bearer ${keyHint.value}"

# => {"balance":980}`,
  },
])

// ---- copy + toast ----
const toastMsg = ref('')
let t = null
function toast(m) { toastMsg.value = m; clearTimeout(t); t = setTimeout(() => (toastMsg.value = ''), 1800) }
async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast('已复制') } catch { toast('复制失败') }
}
</script>

<template>
  <div class="theme-text space-y-10">
    <header>
      <span class="sticker">✦ 开发者</span>
      <h1 class="mt-3 text-4xl md:text-5xl font-bold tracking-tight">接口文档</h1>
      <p class="text-white/45 mt-2">Vivid 协议 — 创建任务后连接保持到任务完成(NDJSON 流),一次性返回终态结果,无需轮询。图像 / 视频 / 参考素材全支持。</p>
    </header>

    <!-- quickstart -->
    <section class="grid md:grid-cols-2 gap-4">
      <div class="card p-6">
        <h2 class="text-sm font-semibold text-white/80">基础信息</h2>
        <dl class="mt-4 space-y-3 text-sm">
          <div class="flex items-center justify-between gap-3">
            <dt class="text-white/45">Base URL</dt><dd class="font-mono text-white/90">{{ base }}</dd>
          </div>
          <div class="flex items-center justify-between gap-3">
            <dt class="text-white/45">鉴权</dt><dd class="font-mono text-white/90">Authorization: Bearer vk_…</dd>
          </div>
          <div class="flex items-center justify-between gap-3">
            <dt class="text-white/45">你的 Key</dt><dd class="font-mono text-white/70">{{ keyHint }}</dd>
          </div>
        </dl>
        <p class="text-[11px] text-white/40 mt-4">还没有 Key?去 <router-link to="/settings" class="text-orange-300 underline">设置 → API Key</router-link> 生成。</p>
      </div>

      <div class="card p-6">
        <h2 class="text-sm font-semibold text-white/80">端点</h2>
        <ul class="mt-4 space-y-2.5 text-sm font-mono">
          <li class="flex items-center gap-2"><span class="badge-post">POST</span><span class="text-white/80">/v1/generate</span><span class="text-white/35 font-sans text-xs">创建任务(NDJSON 流)</span></li>
          <li class="flex items-center gap-2"><span class="badge-post">POST</span><span class="text-white/80">/v1/uploads</span><span class="text-white/35 font-sans text-xs">参考素材上传会话</span></li>
          <li class="flex items-center gap-2"><span class="badge-get">GET</span><span class="text-white/80">/v1/models</span><span class="text-white/35 font-sans text-xs">可售模型 + 能力规格</span></li>
          <li class="flex items-center gap-2"><span class="badge-get">GET</span><span class="text-white/80">/v1/balance</span><span class="text-white/35 font-sans text-xs">查余额</span></li>
        </ul>
      </div>
    </section>

    <!-- models -->
    <section>
      <h2 class="text-lg font-semibold mb-3">可用模型</h2>
      <div class="card overflow-hidden">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-[11px] uppercase tracking-wider text-white/40 border-b border-white/[0.08]">
              <th class="px-4 py-3 font-medium whitespace-nowrap">model</th>
              <th class="px-4 py-3 font-medium whitespace-nowrap">类型</th>
              <th class="px-4 py-3 font-medium whitespace-nowrap" title="视频价 = 分辨率价 + 时长价">定价 <span class="normal-case text-white/30">积分</span></th>
              <th class="px-4 py-3 font-medium">能力</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in models" :key="m.id" class="border-b border-white/[0.04] last:border-0">
              <td class="px-4 py-3 font-mono text-white/90 whitespace-nowrap">{{ pubName(m) }}</td>
              <td class="px-4 py-3 text-white/60 whitespace-nowrap">{{ m.type === 'video' ? '视频' : '图像' }}</td>
              <td class="px-4 py-3">
                <div class="flex flex-wrap items-center gap-1 text-[11px]">
                  <template v-for="r in (m.resolutions || [])" :key="'pr'+r">
                    <span v-if="resPrice(m, r) != null" class="cap cap-price">{{ r }}<b class="ml-1 font-semibold tabular-nums">{{ points(resPrice(m, r)) }}</b></span>
                  </template>
                  <template v-if="m.type === 'video'">
                    <span v-if="perSecondPrice(m) != null" class="cap cap-price">/s<b class="ml-1 font-semibold tabular-nums">{{ points(perSecondPrice(m)) }}</b></span>
                    <template v-else v-for="d in (m.durations || [])" :key="'pd'+d">
                      <span v-if="durPrice(m, d) != null" class="cap cap-price">{{ d }}<b class="ml-1 font-semibold tabular-nums">+{{ points(durPrice(m, d)) }}</b></span>
                    </template>
                  </template>
                  <span v-if="priceEmpty(m)" class="text-white/30">—</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="flex flex-wrap items-center gap-1 text-[11px]">
                  <template v-for="r in (m.resolutions || [])" :key="r">
                    <span class="cap cap-k">{{ r }}</span>
                  </template>
                  <template v-if="m.type === 'video'">
                    <span v-if="durText(m)" class="cap cap-dur">{{ durText(m) }}</span>
                  </template>
                  <span v-if="m.reference_mode === 'frame' && m.max_reference_images > 0" class="cap cap-frame">首尾帧 {{ Math.min(m.max_reference_images, 2) }}</span>
                  <span v-if="m.reference_mode === 'frame' && m.max_reference_images > 2" class="cap cap-ref">参考图 {{ refImages(m) }}</span>
                  <span v-else-if="m.reference_mode && m.reference_mode !== 'none' && m.reference_mode !== 'frame' && m.max_reference_images > 0" class="cap cap-ref">参考图 {{ refImages(m) }}</span>
                  <span v-if="m.type === 'image' && m.image_to_image && (!m.reference_mode || m.reference_mode === 'none')" class="cap cap-ref">参考图 1</span>
                  <template v-if="caps(m)">
                    <span v-if="caps(m).videos" class="cap cap-media">视频 {{ caps(m).videos }}</span>
                    <span v-if="caps(m).audios" class="cap cap-media">音频 {{ caps(m).audios }}</span>
                  </template>
                  <span v-for="r in (m.ratios || [])" :key="'rt'+r" class="cap cap-ratio">{{ r.replace(':', '×') }}</span>
                  <span v-if="modeEmpty(m)" class="text-white/30">—</span>
                </div>
              </td>
            </tr>
            <tr v-if="!models.length"><td colspan="4" class="px-4 py-10 text-center text-white/35">暂无可用模型</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- parameters -->
    <section class="grid lg:grid-cols-2 gap-6">
      <div>
        <h2 class="text-lg font-semibold mb-3">生成参数 <span class="text-xs font-normal text-white/40">POST /v1/generate</span></h2>
        <div class="card overflow-hidden">
          <table class="w-full text-sm">
            <thead><tr class="text-left text-[11px] uppercase tracking-wider text-white/40 border-b border-white/[0.08]">
              <th class="px-4 py-2.5 font-medium">参数</th><th class="px-4 py-2.5 font-medium">类型</th><th class="px-4 py-2.5 font-medium">必填</th><th class="px-4 py-2.5 font-medium">说明</th>
            </tr></thead>
            <tbody>
              <tr v-for="p in generateParams" :key="p[0]" class="border-b border-white/[0.04] last:border-0">
                <td class="px-4 py-2.5 font-mono text-white/85">{{ p[0] }}</td>
                <td class="px-4 py-2.5 text-white/50 font-mono text-xs">{{ p[1] }}</td>
                <td class="px-4 py-2.5 text-white/55">{{ p[2] }}</td>
                <td class="px-4 py-2.5 text-white/60 text-xs">{{ p[3] }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div>
        <h2 class="text-lg font-semibold mb-3">上传参数 <span class="text-xs font-normal text-white/40">POST /v1/uploads</span></h2>
        <div class="card overflow-hidden">
          <table class="w-full text-sm">
            <thead><tr class="text-left text-[11px] uppercase tracking-wider text-white/40 border-b border-white/[0.08]">
              <th class="px-4 py-2.5 font-medium">参数</th><th class="px-4 py-2.5 font-medium">类型</th><th class="px-4 py-2.5 font-medium">必填</th><th class="px-4 py-2.5 font-medium">说明</th>
            </tr></thead>
            <tbody>
              <tr v-for="p in uploadParams" :key="p[0]" class="border-b border-white/[0.04] last:border-0">
                <td class="px-4 py-2.5 font-mono text-white/85">{{ p[0] }}</td>
                <td class="px-4 py-2.5 text-white/50 font-mono text-xs">{{ p[1] }}</td>
                <td class="px-4 py-2.5 text-white/55">{{ p[2] }}</td>
                <td class="px-4 py-2.5 text-white/60 text-xs">{{ p[3] }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="text-xs text-white/40 mt-3">
          用法:POST 拿 <code class="text-white/70">url</code> → PUT 直传 → 返回的 <code class="text-white/70">key</code> 放进
          <code class="text-white/70">/v1/generate</code> 的 <code class="text-white/70">refs</code>。
          预签名 URL 仅用于 PUT(不可 GET 读回),30 分钟内有效。
          支持:图 png/jpeg/webp,视频 mp4/webm/mov,音频 mp3/wav/m4a/ogg。
        </p>
      </div>
    </section>

    <!-- examples -->
    <section class="space-y-4">
      <h2 class="text-lg font-semibold">调用示例</h2>
      <div v-for="ex in examples" :key="ex.title" class="code-card">
        <div class="code-card-bar">
          <span class="code-dots" aria-hidden="true"><i class="dot dot-r"></i><i class="dot dot-y"></i><i class="dot dot-g"></i></span>
          <span class="code-card-title">{{ ex.title }}</span>
          <button @click="copy(ex.code)" class="code-copy">
            <Icon name="copy" class="w-3.5 h-3.5" /> 复制
          </button>
        </div>
        <pre class="code-card-body"><code>{{ ex.code }}</code></pre>
      </div>
    </section>

    <!-- responses -->
    <section>
      <h2 class="text-lg font-semibold mb-3">响应 & 计费</h2>
      <div class="card p-6 space-y-3 text-sm text-white/70">
        <p><strong class="text-white/90">NDJSON 流</strong>(<code class="text-white/85 font-mono">application/x-ndjson</code>):连接建立即回执首行
          <code class="text-white/85 font-mono">{{ '{"jobId":"…","status":"running"}' }}</code>;
          等待期间每 25 秒一个空行心跳(请忽略);末行为终态
          <code class="text-white/85 font-mono">succeeded</code> / <code class="text-white/85 font-mono">failed</code> —— 读最后一行 JSON 即结果。</p>
        <p><strong class="text-white/90">断流重连</strong>:流意外断开时,只带 <code class="text-white/85 font-mono">jobId</code> 重发同一请求即可重连续等 —— 不会重复创建任务、不重复扣费;已完成直接回终态帧。</p>
        <p><strong class="text-white/90">产物 URL</strong>:<code class="text-white/85 font-mono">result</code> 是只读预签名 URL(30 分钟),过期即焚,请及时下载转存。任务类型即文件类型。</p>
        <p><strong class="text-white/90">计费(预扣)</strong>:生成<strong class="text-white/90">前</strong>按上表价格从你的 Key 账号预扣积分;终态 <code class="text-white/85 font-mono">failed</code> 时 <code class="text-white/85 font-mono">refunded:true</code>,预扣积分自动退回 —— 失败不扣费。</p>
        <div class="pt-2 grid sm:grid-cols-2 gap-2 text-xs">
          <div class="flex items-center gap-2"><span class="badge-err">4001</span> 参数/业务拒绝:模型不可用、quality 不在白名单、refs 超限等</div>
          <div class="flex items-center gap-2"><span class="badge-err">4002</span> 积分不足,请先充值</div>
          <div class="flex items-center gap-2"><span class="badge-err">4011</span> API Key 无效或未授权</div>
          <div class="flex items-center gap-2"><span class="badge-err">4293</span> 并发任务数已达上限 / 上游繁忙,请重试</div>
          <div class="flex items-center gap-2"><span class="badge-err">5001</span> 服务异常,请稍后重试</div>
        </div>
      </div>
    </section>

    <transition name="fade">
      <div v-if="toastMsg" class="fixed bottom-8 left-1/2 -translate-x-1/2 z-50 bg-white text-black text-sm font-medium px-5 py-2.5 rounded-full shadow-2xl">{{ toastMsg }}</div>
    </transition>
  </div>
</template>

<style scoped>
.badge-get, .badge-post, .badge-err {
  border-radius: 4px; padding: 2px 6px; font-size: 10px; line-height: 1;
}
.badge-get { background: rgb(16 185 129 / 0.14); color: rgb(4 120 87); box-shadow: inset 0 0 0 1px rgb(16 185 129 / 0.35); }
.badge-post { background: rgb(14 165 233 / 0.14); color: rgb(3 105 161); box-shadow: inset 0 0 0 1px rgb(14 165 233 / 0.35); }
.badge-err { background: rgb(244 63 94 / 0.12); color: rgb(190 18 60); box-shadow: inset 0 0 0 1px rgb(244 63 94 / 0.3); font-family: ui-monospace, monospace; }
html.dark .badge-get { background: rgb(16 185 129 / 0.15); color: rgb(110 231 183); box-shadow: inset 0 0 0 1px rgb(52 211 153 / 0.3); }
html.dark .badge-post { background: rgb(14 165 233 / 0.15); color: rgb(125 211 252); box-shadow: inset 0 0 0 1px rgb(56 189 248 / 0.3); }
html.dark .badge-err { background: rgb(244 63 94 / 0.15); color: rgb(253 164 175); box-shadow: inset 0 0 0 1px rgb(251 113 133 / 0.3); }

.cap { display: inline-flex; align-items: center; padding: 0.18rem 0.55rem; font-size: 0.7rem; border-radius: 9999px; white-space: nowrap; }
.cap-k { background: rgb(16 185 129 / 0.12); color: rgb(4 120 87); box-shadow: inset 0 0 0 1px rgb(16 185 129 / 0.3); }
.cap-dur { background: rgb(14 165 233 / 0.12); color: rgb(55 48 163); box-shadow: inset 0 0 0 1px rgb(14 165 233 / 0.3); }
.cap-frame { background: rgb(251 113 133 / 0.12); color: rgb(159 18 57); box-shadow: inset 0 0 0 1px rgb(251 113 133 / 0.3); }
.cap-ref { background: rgb(16 185 129 / 0.12); color: rgb(4 120 87); box-shadow: inset 0 0 0 1px rgb(16 185 129 / 0.3); }
.cap-media { background: rgb(245 158 11 / 0.12); color: rgb(146 64 14); box-shadow: inset 0 0 0 1px rgb(245 158 11 / 0.3); }
.cap-price { background: rgb(14 165 233 / 0.12); color: rgb(3 105 161); box-shadow: inset 0 0 0 1px rgb(14 165 233 / 0.3); font-variant-numeric: tabular-nums; }
.cap-ratio { background: rgb(100 116 139 / 0.1); color: rgb(51 65 85); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; box-shadow: inset 0 0 0 1px rgb(100 116 139 / 0.25); }
html.dark .cap-k { background: rgb(16 185 129 / 0.18); color: rgb(110 231 183); box-shadow: inset 0 0 0 1px rgb(52 211 153 / 0.4); }
html.dark .cap-dur { background: rgb(14 165 233 / 0.18); color: rgb(147 197 253); box-shadow: inset 0 0 0 1px rgb(129 140 248 / 0.4); }
html.dark .cap-frame { background: rgb(251 113 133 / 0.18); color: rgb(244 114 182); box-shadow: inset 0 0 0 1px rgb(244 114 182 / 0.45); }
html.dark .cap-ref { background: rgb(16 185 129 / 0.18); color: rgb(110 231 183); box-shadow: inset 0 0 0 1px rgb(52 211 153 / 0.4); }
html.dark .cap-media { background: rgb(245 158 11 / 0.18); color: rgb(252 211 77); box-shadow: inset 0 0 0 1px rgb(252 211 77 / 0.45); }
html.dark .cap-price { background: rgb(14 165 233 / 0.18); color: rgb(125 211 252); box-shadow: inset 0 0 0 1px rgb(56 189 248 / 0.45); }
html.dark .cap-ratio { background: rgb(255 255 255 / 0.06); color: rgb(255 255 255 / 0.65); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.12); }

/* 苹果风代码卡片：自带深色配色，不随页面主题切换，深/浅背景下都可读 */
.code-card {
  border-radius: 14px; overflow: hidden;
  background: #1d1d1f;
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.25), 0 8px 24px rgb(0 0 0 / 0.18), inset 0 0 0 1px rgb(255 255 255 / 0.08);
}
.code-card-bar {
  display: flex; align-items: center; gap: 10px;
  padding: 9px 12px;
  background: linear-gradient(180deg, #333336, #2a2a2d);
  border-bottom: 1px solid rgb(0 0 0 / 0.4);
}
.code-dots { display: inline-flex; gap: 6px; }
.code-dots .dot { width: 11px; height: 11px; border-radius: 9999px; display: inline-block; }
.dot-r { background: #ff5f57; box-shadow: inset 0 0 0 0.5px rgb(0 0 0 / 0.15); }
.dot-y { background: #febc2e; box-shadow: inset 0 0 0 0.5px rgb(0 0 0 / 0.15); }
.dot-g { background: #28c840; box-shadow: inset 0 0 0 0.5px rgb(0 0 0 / 0.15); }
.code-card-title {
  flex: 1; min-width: 0; text-align: center;
  font-size: 12px; color: rgb(255 255 255 / 0.55);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.code-copy {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 12px; line-height: 1;
  color: rgb(255 255 255 / 0.6);
  background: rgb(255 255 255 / 0.08);
  border-radius: 7px; padding: 5px 9px;
  transition: background 0.15s, color 0.15s;
}
.code-copy:hover { color: #fff; background: rgb(255 255 255 / 0.16); }
.code-card-body {
  margin: 0; padding: 14px 16px;
  font-size: 12px; line-height: 1.7;
  color: #e8e8ed;
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
