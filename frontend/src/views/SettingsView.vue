<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { auth, refreshMe, logout as authLogout } from '../auth'
import { api, jsonBody } from '../api'
import { openPayment } from '../payment'
import Icon from '../components/Icon.vue'
import { points, pointsLabel } from '../credits'
import { site } from '../site'

const router = useRouter()

// Pull the latest server-side credits + check-in state when the page opens
// (so an admin adjustment, or a check-in from another tab, shows up).
onMounted(refreshMe)

// Hide the check-in card when the admin has disabled the feature, and read the
// daily reward amount from the same config (admin-configurable, not hardcoded).
const checkinEnabled = ref(true)
const checkinReward = ref(3)   // fallback until /auth/config loads
onMounted(async () => {
  const r = await api('/auth/config')
  if (r.ok) {
    checkinEnabled.value = r.data.checkin_enabled !== false
    checkinReward.value = Number(r.data.checkin_reward) || 0
  }
})

// ---- API Key (REAL: minted + verified server-side; vivid /v1 protocol) ----
// The full plaintext is returned only once, right after minting. On reload we
// only have the server's masked preview (the plaintext is never stored).
const apiKey = ref('')        // full plaintext — present only right after minting
const keyPreview = ref('')    // masked preview from the server (persists)
const apiKeyRevealed = ref(false)
const hasKey = computed(() => !!apiKey.value || !!keyPreview.value)

async function loadKey() {
  const r = await api('/auth/api-key')
  if (r.ok) keyPreview.value = r.data?.key?.key_preview || ''
}
onMounted(loadKey)

// Uniform mask for the hidden / persisted state — always "***" so the just-minted
// preview and the server-stored preview read the same (no vk_xxx…yyyy mismatch).
const maskedKey = computed(() => (apiKey.value || keyPreview.value) ? '***' : '')

async function generateKey() {
  if (hasKey.value && !confirm('已有 Key — 重新生成会让旧 Key 立刻失效,确认?')) return
  const r = await api('/auth/api-key', jsonBody('POST', {}))
  if (!r.ok) { toast(r.data?.detail || '生成失败'); return }
  apiKey.value = r.data.key
  keyPreview.value = r.data.preview || ''
  apiKeyRevealed.value = true
  toast('新 Key 已生成 — 请立刻复制保存(只显示这一次)')
}

// Only the just-minted plaintext is copyable — the server-side preview is a mask
// (…abcd), so copying it would hand out a key that can never authenticate.
async function copyKey() {
  if (!apiKey.value) {
    toast('完整 Key 仅生成时显示一次,已无法找回 — 请重新生成')
    return
  }
  try {
    await navigator.clipboard.writeText(apiKey.value)
    toast('已复制完整 Key')
  } catch { toast('复制失败') }
}

async function clearKey() {
  if (!confirm('清除 Key? 之后用该 Key 的调用都会失败。')) return
  const r = await api('/auth/api-key', { method: 'DELETE' })
  if (!r.ok) { toast(r.data?.detail || '清除失败'); return }
  apiKey.value = ''; keyPreview.value = ''; apiKeyRevealed.value = false
  toast('已清除 API Key')
}

// ---- Password (real: verifies current password server-side) ----
const pwdForm = ref({ current: '', next: '', confirm: '' })
const pwdSubmitting = ref(false)
const pwdError = ref('')
async function changePwd() {
  pwdError.value = ''
  if (!pwdForm.value.current) { pwdError.value = '请输入当前密码'; return }
  const next = pwdForm.value.next || ''
  if (next.length < 8 || next.length > 24) { pwdError.value = '新密码长度需为 8-24 位'; return }
  if (!/[A-Z]/.test(next) || !/[a-z]/.test(next) || !/\d/.test(next) || !/[()~!@#$%^&*\-_=|{}\[\]:;'<>.,?/]/.test(next)) {
    pwdError.value = '新密码必须包含大写字母、小写字母、数字和符号'
    return
  }
  if (pwdForm.value.next !== pwdForm.value.confirm) { pwdError.value = '两次输入的新密码不一致'; return }
  pwdSubmitting.value = true
  const r = await api('/auth/change-password', jsonBody('POST', {
    current_password: pwdForm.value.current,
    new_password: pwdForm.value.next,
  }))
  pwdSubmitting.value = false
  if (!r.ok) { pwdError.value = r.data?.detail || '修改失败'; return }
  pwdForm.value = { current: '', next: '', confirm: '' }
  toast('密码已更新')
}

// ---- Credits: the REAL server-side balance of the logged-in user ----
// (admin adjustments in 用户管理 write this same field). Top-up is via CDK.
const balance = computed(() => Number(auth.user?.credits || 0))
const roleLabel = computed(() => ({ user: '普通用户', agent: '代理', admin: '管理员' }[auth.user?.role] || '普通用户'))
const concurrencyLabel = computed(() => {
  const n = Number(auth.user?.concurrency_limit || 0)
  const g = auth.user?.concurrency_group
  if (n <= 0) return g ? `不限制 · ${g}` : '不限制'
  return g ? `${n} · ${g}` : String(n)
})

const cdkCode = ref('')
const cdkBusy = ref(false)
const cdkError = ref('')
async function redeemCdk() {
  cdkError.value = ''
  const code = cdkCode.value.trim()
  if (!code) { cdkError.value = '请输入兑换码'; return }
  cdkBusy.value = true
  const r = await api('/auth/redeem-cdk', jsonBody('POST', { code }))
  cdkBusy.value = false
  if (!r.ok) { cdkError.value = r.data?.detail || '兑换失败'; return }
  if (auth.user) auth.user.credits = r.data.credits
  cdkCode.value = ''
  toast(`兑换成功 +${Number(r.data.amount).toLocaleString('en-US')} 积分`)
}

// ---- Daily check-in ----
// Reward amount lives in `checkinReward` (loaded from /auth/config above).

// Streak + "checked today" come from the server account (date authority is
// the backend, so timezone quirks can't desync the disabled state).
const streak = computed(() => Number(auth.user?.checkin_streak || 0))
const checkedToday = computed(() => !!auth.user?.checkin_today)
const last7 = computed(() => {
  const out = []
  const today = new Date()
  for (let i = 6; i >= 0; i--) {
    const d = new Date(today); d.setDate(today.getDate() - i)
    const ds = d.toISOString().slice(0, 10)
    const isToday = i === 0
    // if streak >= i+1 and today checked, then day (today - i) was within streak
    const lit = checkedToday.value && i < streak.value
    out.push({ ds, day: d.getDate(), isToday, lit })
  }
  return out
})

async function checkin() {
  if (checkedToday.value) return
  const r = await api('/auth/checkin', jsonBody('POST', {}))
  if (!r.ok) { toast(r.data?.detail || '签到失败'); return }
  await refreshMe()   // refresh real balance + streak + checkin_today
  const d = r.data || {}
  if (d.already) { toast('今日已签到'); return }
  toast(`签到成功 +${pointsLabel(d.awarded)} → 余额 ${pointsLabel(balance.value)}`)
}

// ---- Logout (real: ends the server session, then returns home) ----
async function logout() {
  if (!confirm('确定要退出登录?')) return
  await authLogout()
  // Clear local-only leftovers (API key + legacy demo keys). Credits/streak are
  // computed from auth.user, which authLogout() has already cleared.
  const stale = ['gw_api_key', 'gw_credits', 'gw_checkin_last', 'gw_checkin_streak',
    'gw_invite_code', 'gw_invite_count', 'gw_invite_earned']
  stale.forEach((k) => localStorage.removeItem(k))
  apiKey.value = ''
  toast('已退出登录')
  setTimeout(() => router.push('/'), 600)
}

// ---- Toast ----
const toastMsg = ref('')
let toastTimer = null
function toast(m) {
  toastMsg.value = m
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toastMsg.value = ''), 2200)
}

// ---- Recharge (易支付) ----
const payCfg = ref({ enabled: false, methods: [], min_amount: 0, points_ratio: 100 })
const AMOUNTS = [10, 20, 50, 100]
const picked = ref(10)            // a preset number, or 'custom'
const customAmount = ref('')
const payMethod = ref('')
const rechargeTotal = computed(() => Number(auth.user?.recharge_total || 0))
const methodName = (m) => ({ wxpay: '微信', alipay: '支付宝' }[m] || m)
const finalAmount = computed(() => Number(picked.value === 'custom' ? customAmount.value : picked.value) || 0)
const pointsPreview = computed(() => Math.round(finalAmount.value * (payCfg.value.points_ratio || 0)))
async function loadPayCfg() {
  const r = await api('/pay/config')
  if (r.ok && r.data) {
    payCfg.value = r.data
    if (r.data.methods?.length && !payMethod.value) payMethod.value = r.data.methods[0]
  }
}
onMounted(loadPayCfg)
const recharging = ref(false)
async function recharge() {
  if (recharging.value) return
  const amt = finalAmount.value
  if (!amt || amt <= 0) { toast('请输入有效金额'); return }
  if (amt < payCfg.value.min_amount) { toast(`最低充值 ${payCfg.value.min_amount} 元`); return }
  if (!payMethod.value) { toast('请选择支付方式'); return }
  recharging.value = true
  try {
    const r = await api('/pay/recharge', jsonBody('POST', { amount: amt, method: payMethod.value }))
    if (!r.ok) { toast(r.data?.detail || '下单失败'); return }
    openPayment(r.data, { onPaid: refreshMe })
  } finally {
    recharging.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <div class="page-head">
      <div>
        <h2 class="page-title">设置</h2>
        <p class="page-sub">API Key、密码、积分、签到 — 账户全在这里。</p>
      </div>
    </div>

    <!-- ===== 账户信息 ===== -->
    <section class="card p-6">
      <div class="sec-head">
        <span class="sec-ico"><Icon name="accounts" :size="16" /></span>
        <h3>账户信息</h3>
      </div>
      <div class="grid grid-cols-2 md:grid-cols-3 gap-x-6 gap-y-5">
        <div class="st-item">
          <div class="st-label">用户名</div>
          <div class="st-value truncate" :title="auth.user?.name || ''">{{ auth.user?.name || '—' }}</div>
        </div>
        <div class="st-item">
          <div class="st-label">绑定邮箱</div>
          <div class="st-value break-all" :title="auth.user?.email || ''">{{ auth.user?.email || '—' }}</div>
        </div>
        <div class="st-item">
          <div class="st-label">角色</div>
          <div class="st-value">{{ roleLabel }}</div>
        </div>
        <div class="st-item">
          <div class="st-label">积分余额</div>
          <div class="st-value st-num text-amber-600">{{ pointsLabel(balance) }}</div>
        </div>
        <div class="st-item">
          <div class="st-label">累计充值</div>
          <div class="st-value st-num text-emerald-600">¥{{ rechargeTotal }}</div>
        </div>
        <div class="st-item">
          <div class="st-label">并发上限</div>
          <div class="st-value truncate" :title="concurrencyLabel">{{ concurrencyLabel }}</div>
        </div>
      </div>
    </section>

    <!-- ===== API Key ===== -->
    <section class="card p-6">
      <div class="grid md:grid-cols-[220px_1fr] gap-6 items-start">
        <div>
          <div class="sec-head">
            <span class="sec-ico"><Icon name="plug" :size="16" /></span>
            <h3>API Key</h3>
          </div>
          <p class="text-[13px] text-[color:var(--fg-3)] leading-relaxed mt-1">
            调用 <code class="key-code">/v1/*</code> 接口的访问密钥。完整密钥不保存，仅生成时显示一次。
          </p>
        </div>
        <div>
          <div v-if="hasKey" class="key-box">
            <code class="flex-1 font-mono text-[13px] text-[color:var(--fg)] break-all">{{ apiKeyRevealed && apiKey ? apiKey : maskedKey }}</code>
            <button v-if="apiKey" @click="apiKeyRevealed = !apiKeyRevealed" class="btn-soft !h-8">{{ apiKeyRevealed ? '隐藏' : '显示' }}</button>
            <button @click="copyKey" :title="apiKey ? '复制完整 Key' : '完整 Key 仅生成时显示一次，已无法找回'"
                    class="btn-soft !h-8" :class="!apiKey && 'opacity-40'">复制</button>
          </div>
          <div v-else class="rounded-xl border border-dashed border-[color:var(--hairline)] px-4 py-5 text-center text-xs text-[color:var(--fg-faint)]">
            还没有 Key — 点下面的「生成」按钮自动生成
          </div>
          <p v-if="apiKey" class="text-[11px] text-amber-600 mt-2">⚠ 完整 Key 仅显示这一次，请立刻复制保存。</p>
          <div class="mt-3 flex gap-2">
            <button @click="generateKey" class="btn-primary">{{ hasKey ? '重新生成' : '生成 Key' }}</button>
            <button v-if="hasKey" @click="clearKey" class="btn-danger-soft">清除</button>
          </div>
          <p class="text-[11px] text-[color:var(--fg-faint)] mt-3">
            Key 由系统随机生成，不能手动填写。重新生成会让旧 Key 立刻失效。完整调用示例见
            <router-link to="/docs" class="underline underline-offset-2">接口文档</router-link>。
          </p>
        </div>
      </div>
    </section>

    <div class="grid lg:grid-cols-2 gap-5">
      <!-- ===== 每日签到 ===== -->
      <section v-if="checkinEnabled" class="card p-6">
        <div class="sec-head">
          <span class="sec-ico"><Icon name="refresh" :size="16" /></span>
          <h3>每日签到</h3>
        </div>
        <p class="text-[13px] text-[color:var(--fg-3)] mt-1">每天签到 +{{ checkinReward }} 积分。</p>
        <div class="mt-4 flex items-baseline gap-2">
          <span class="text-3xl font-extrabold tabular-nums tracking-tight">{{ streak }}</span>
          <span class="text-xs text-[color:var(--fg-3)]">天连续</span>
        </div>
        <div class="mt-4 flex items-center gap-1.5">
          <div v-for="(d, i) in last7" :key="d.ds" class="ck-day"
               :class="d.lit || (d.isToday && checkedToday) ? 'ck-lit' : d.isToday ? 'ck-today' : ''">
            <Icon v-if="d.lit || (d.isToday && checkedToday)" name="spark" :size="12" />
            <span v-else class="text-[10px]">{{ d.isToday ? '今' : i + 1 }}</span>
          </div>
        </div>
        <button @click="checkin" :disabled="checkedToday" class="btn-primary w-full mt-5 !py-2.5">
          {{ checkedToday ? '今日已签到 · 明天再来' : `立即签到 +${checkinReward} 积分` }}
        </button>
      </section>

      <!-- ===== 积分充值 ===== -->
      <section v-if="payCfg.enabled" class="card p-6">
        <div class="sec-head">
          <span class="sec-ico"><Icon name="spark" :size="16" /></span>
          <h3>积分充值</h3>
        </div>
        <p class="text-[13px] text-[color:var(--fg-3)] mt-1">累计充值 <strong class="text-emerald-600">¥{{ rechargeTotal }}</strong> · {{ payCfg.points_ratio }} 积分 / 元</p>
        <div class="grid grid-cols-3 sm:grid-cols-5 gap-2 mt-4">
          <button v-for="a in AMOUNTS" :key="a" @click="picked = a" class="amt" :class="picked === a && 'amt-on'">{{ a }}元</button>
          <button @click="picked = 'custom'" class="amt" :class="picked === 'custom' && 'amt-on'">自定义</button>
        </div>
        <input v-if="picked === 'custom'" v-model="customAmount" type="number" min="1" step="1" placeholder="输入金额（元）"
               class="field mt-3" />
        <div class="flex gap-2 mt-3">
          <button v-for="m in payCfg.methods" :key="m" @click="payMethod = m" class="amt flex-1" :class="payMethod === m && 'amt-on'">{{ methodName(m) }}</button>
        </div>
        <div class="mt-5 flex items-center justify-between gap-3">
          <span class="text-sm text-[color:var(--fg-2)]">到账 <strong class="text-amber-600 text-base tabular-nums">{{ pointsPreview }}</strong> 积分</span>
          <button @click="recharge" :disabled="recharging" class="btn-primary">
            <span v-if="recharging" class="w-3.5 h-3.5 rounded-full border-2 border-white/30 border-t-white animate-spin"></span>
            {{ recharging ? '下单中…' : '立即充值' }}
          </button>
        </div>
      </section>

      <!-- ===== 兑换码 ===== -->
      <section v-if="site.cdkRedeemEnabled" class="card p-6">
        <div class="sec-head">
          <span class="sec-ico"><Icon name="spark" :size="16" /></span>
          <h3>兑换码充值</h3>
        </div>
        <div class="mt-3 flex items-baseline gap-2">
          <span class="text-3xl font-extrabold tabular-nums tracking-tight">{{ points(balance).toLocaleString('en-US') }}</span>
          <span class="text-xs text-[color:var(--fg-3)]">积分余额</span>
        </div>
        <div class="mt-4 flex gap-2">
          <input v-model="cdkCode" @keyup.enter="redeemCdk" placeholder="输入兑换码（CDK）"
                 class="field flex-1 font-mono uppercase tracking-wider" />
          <button @click="redeemCdk" :disabled="cdkBusy" class="btn-primary shrink-0">{{ cdkBusy ? '兑换中…' : '兑换' }}</button>
        </div>
        <p v-if="cdkError" class="text-xs text-rose-600 mt-2">{{ cdkError }}</p>
        <p class="text-[11px] text-[color:var(--fg-faint)] mt-3">兑换码每个仅可使用一次。还没有兑换码？到商店购买后回此处充值。</p>
        <a v-if="site.contact?.shop" :href="site.contact.shop" target="_blank" rel="noopener" class="btn-soft mt-3">
          前往商店购买兑换码 <span aria-hidden="true">→</span>
        </a>
      </section>

      <!-- ===== 修改密码 ===== -->
      <section class="card p-6">
        <div class="sec-head">
          <span class="sec-ico"><Icon name="config" :size="16" /></span>
          <h3>修改密码</h3>
        </div>
        <p class="text-[13px] text-[color:var(--fg-3)] mt-1">8-24 位，必须包含大写字母、小写字母、数字和符号。</p>
        <div class="mt-4 space-y-3">
          <input v-model="pwdForm.current" type="password" placeholder="当前密码" autocomplete="current-password" class="field" />
          <input v-model="pwdForm.next" type="password" placeholder="新密码（8-24 位，含大小写 / 数字 / 符号）" autocomplete="new-password" class="field" />
          <input v-model="pwdForm.confirm" type="password" placeholder="再次输入新密码" autocomplete="new-password" class="field" />
          <p v-if="pwdError" class="text-xs text-rose-600">{{ pwdError }}</p>
          <button @click="changePwd" :disabled="pwdSubmitting" class="btn-primary w-full !py-2.5">{{ pwdSubmitting ? '提交中…' : '更新密码' }}</button>
        </div>
      </section>
    </div>

    <!-- ===== 退出登录 ===== -->
    <section class="card p-5 flex items-center gap-4 flex-wrap">
      <span class="sec-ico"><Icon name="close" :size="16" /></span>
      <div class="flex-1 min-w-0">
        <div class="text-sm font-bold">退出登录</div>
        <p class="text-xs text-[color:var(--fg-3)] mt-0.5">会清除本地 API Key 与缓存，下次进入需要重新登录。</p>
      </div>
      <button @click="logout" class="btn-danger-soft">退出</button>
    </section>

    <!-- toast -->
    <transition name="fade">
      <div v-if="toastMsg"
           class="fixed bottom-8 left-1/2 -translate-x-1/2 z-50 bg-zinc-900 text-white text-sm font-medium px-5 py-2.5 rounded-full shadow-2xl">
        {{ toastMsg }}
      </div>
    </transition>
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; transform: translateY(8px); }

.sec-head { display: flex; align-items: center; gap: 8px; }
.sec-head h3 { font-size: 15px; font-weight: 800; letter-spacing: -0.01em; color: var(--fg); }
.sec-ico {
  width: 34px; height: 34px; flex: none; border-radius: 10px;
  display: grid; place-items: center;
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  color: var(--fg-2);
}
.st-label { font-size: 11px; font-weight: 700; letter-spacing: .05em; color: var(--fg-faint); margin-bottom: 9px; }
.st-value { font-size: 14.5px; font-weight: 600; color: var(--fg); line-height: 1.5; }
.st-num { font-variant-numeric: tabular-nums; }
.key-code { background: var(--surface-2); padding: 1px 6px; border-radius: 5px; font-size: 12px; }
.key-box {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 8px 8px 14px; border-radius: 12px;
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
}
.btn-danger-soft {
  display: inline-flex; align-items: center; justify-content: center; gap: 6px;
  height: 36px; padding: 0 16px; border-radius: 10px;
  font-size: 13px; font-weight: 700;
  color: rgb(190 18 60); background: rgb(244 63 94 / 0.08);
  box-shadow: inset 0 0 0 1px rgb(244 63 94 / 0.22);
  transition: background .15s ease, color .15s ease;
}
.btn-danger-soft:hover { color: #fff; background: rgb(225 29 72); }
.ck-day {
  flex: 1; height: 46px; border-radius: 10px;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  font-weight: 600;
  background: var(--surface-2); color: var(--fg-faint);
  box-shadow: inset 0 0 0 1px var(--hairline);
}
.ck-today { color: var(--fg-2); box-shadow: inset 0 0 0 1.5px rgb(24 24 27 / 0.4); }
.ck-lit { background: #18181b; color: #fff; box-shadow: none; }

/* Recharge amount / method buttons */
.amt {
  border-radius: 0.6rem;
  padding: 0.6rem 0;
  font-size: 0.875rem;
  text-align: center;
  color: var(--fg-2);
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--hairline);
  transition: background 0.15s, color 0.15s, box-shadow 0.15s;
}
.amt:hover { color: var(--fg); background: var(--hover); }
.amt-on { background: var(--btn-solid-bg); color: var(--btn-solid-fg); box-shadow: none; }
</style>