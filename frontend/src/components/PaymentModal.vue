<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import QRCode from 'qrcode'
import { api } from '../api'
import { refreshMe } from '../auth'
import { payment, closePayment } from '../payment'
import Icon from './Icon.vue'

const qrUrl = ref('')
const remaining = ref(0)     // seconds until expiry
const paid = ref(false)
const closeIn = ref(5)       // post-paid auto-close countdown
let pollTimer = null, tickTimer = null, closeTimer = null
let serverOffset = 0 // (client epoch − server epoch); makes the countdown clock-skew-proof

const order = computed(() => payment.order || {})
const isJump = computed(() => order.value.pay_info_type === 'jump')
// Dead = the order can no longer be paid: cancelled server-side, or the countdown
// has hit zero (the QR is invalid even before the sweep flips the status).
const dead = computed(() => !paid.value && (order.value.status === 'cancelled' || remaining.value <= 0))
const methodLabel = computed(() => ({ wxpay: '微信支付', alipay: '支付宝' }[order.value.pay_type] || order.value.pay_type || ''))
const statusLabel = computed(() => ({ pending: '待支付', paid: '已支付', cancelled: '已取消' }[order.value.status] || order.value.status))
const mmss = computed(() => {
  const s = Math.max(0, remaining.value)
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
})
function fmtTime(unix) {
  if (!unix) return '—'
  const d = new Date(unix * 1000)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function clearTimers() {
  clearInterval(pollTimer); clearInterval(tickTimer); clearInterval(closeTimer)
  pollTimer = tickTimer = closeTimer = null
}

async function renderQR() {
  qrUrl.value = ''
  const info = order.value.pay_info
  if (!info) return
  try { qrUrl.value = await QRCode.toDataURL(info, { width: 220, margin: 1 }) } catch { qrUrl.value = '' }
}

async function poll() {
  if (!order.value.id || paid.value) return
  const r = await api(`/pay/orders/${order.value.id}`)
  if (r.ok && r.data) {
    payment.order = { ...payment.order, ...r.data }
    if (r.data.status === 'paid') onPaid()
    else if (r.data.status === 'cancelled') { clearInterval(pollTimer); pollTimer = null }
  }
}

function onPaid() {
  if (paid.value) return
  paid.value = true
  clearTimers()
  refreshMe() // pull the new credits / 累计充值
  closeIn.value = 5
  closeTimer = setInterval(() => {
    closeIn.value -= 1
    if (closeIn.value <= 0) finish()
  }, 1000)
}

function finish() {
  clearTimers()
  // Only run the onPaid callback (e.g. navigate to 设置) when the order was
  // actually paid — closing the popup without paying must not navigate.
  const cb = paid.value ? payment.onPaid : null
  closePayment()
  if (cb) cb()
}

function tick() {
  const serverNow = Date.now() / 1000 - serverOffset
  remaining.value = Math.max(0, Math.floor((order.value.expires_at || 0) - serverNow))
}

watch(() => payment.show, (show) => {
  clearTimers()
  paid.value = false
  if (!show) return
  // Anchor the countdown to the server clock so a wrong local clock can't make
  // 30 minutes read as 32.
  serverOffset = order.value.server_now ? (Date.now() / 1000 - order.value.server_now) : 0
  if (order.value.status === 'paid') { onPaid(); return }
  if (isJump.value && order.value.pay_info) {
    window.open(order.value.pay_info, '_blank') // auto-jump to the cashier
  } else {
    renderQR()
  }
  tick(); tickTimer = setInterval(tick, 1000)
  poll(); pollTimer = setInterval(poll, 3000)
})

onUnmounted(clearTimers)
</script>

<template>
  <transition name="pay-fade">
    <div v-if="payment.show" class="fixed inset-0 z-[85] bg-slate-950/70 backdrop-blur-sm flex items-center justify-center p-4"
         @click.self="!paid && finish()">
      <div class="w-full max-w-sm rounded-xl bg-white text-slate-800 border-[3px] border-[#111] shadow-[8px_8px_0_#111] overflow-hidden">
        <div class="px-5 py-4 border-b-[2.5px] border-b-[#111] flex items-center justify-between">
          <h2 class="text-base font-semibold">{{ paid ? '支付成功' : dead ? '支付超时' : (isJump ? '支付监控中' : '扫码支付') }}</h2>
          <button v-if="!paid" @click="finish" class="text-slate-400 hover:text-slate-700"><Icon name="close" class="w-5 h-5" /></button>
        </div>

        <!-- paid -->
        <div v-if="paid" class="px-6 py-10 text-center">
          <div class="w-16 h-16 mx-auto rounded-full bg-[#a7f3d0] text-emerald-700 border-[2.5px] border-[#111] shadow-[4px_4px_0_#111] grid place-items-center mb-4">
            <Icon name="spark" class="w-8 h-8" />
          </div>
          <p class="text-lg font-semibold text-slate-800">充值成功</p>
          <p class="text-sm text-slate-500 mt-1">已到账 <strong class="text-emerald-600">{{ order.points }}</strong> 积分</p>
          <p class="text-xs text-slate-400 mt-4">{{ closeIn }} 秒后自动关闭</p>
        </div>

        <!-- expired / cancelled -->
        <div v-else-if="dead" class="px-6 py-10 text-center">
          <div class="w-16 h-16 mx-auto rounded-full bg-slate-100 text-slate-400 grid place-items-center mb-4">
            <Icon name="close" class="w-8 h-8" />
          </div>
          <p class="text-lg font-semibold text-slate-700">支付超时</p>
          <p class="text-sm text-slate-500 mt-1">二维码已失效,订单已取消</p>
          <p class="text-xs text-slate-400 mt-1">如需充值请重新下单</p>
          <button @click="finish" class="mt-5 rounded-lg bg-[#ec5b13] text-white border-[2.5px] border-[#111] shadow-[3px_3px_0_#111] hover:-translate-y-0.5 active:translate-y-0.5 active:shadow-none px-5 py-2 text-sm font-extrabold transition-all">关闭</button>
        </div>

        <!-- pending -->
        <div v-else class="px-6 py-5">
          <!-- qrcode: scan to pay -->
          <template v-if="!isJump">
            <div class="flex justify-center mb-3">
              <div class="w-[228px] h-[228px] rounded-xl border-[2.5px] border-[#111] shadow-[4px_4px_0_#111] grid place-items-center overflow-hidden bg-white p-3.5">
                <img v-if="qrUrl" :src="qrUrl" alt="支付二维码" class="w-full h-full rounded-lg" />
                <span v-else class="text-xs text-slate-400">二维码生成中…</span>
              </div>
            </div>
            <p class="text-center text-xs text-slate-500 mb-4">请使用<strong class="text-slate-700">{{ methodLabel }}</strong>扫码支付</p>
          </template>
          <!-- jump (no qrcode): the cashier opened in a new tab — just monitor -->
          <template v-else>
            <div class="flex flex-col items-center justify-center py-7 mb-2">
              <div class="w-12 h-12 rounded-full border-[3px] border-[#fecaca] border-t-[#ec5b13] animate-spin mb-3"></div>
              <p class="text-sm font-medium text-slate-700">支付监控中…</p>
              <p class="text-xs text-slate-400 mt-1">已打开支付页面,完成后自动到账</p>
            </div>
          </template>

          <dl class="text-sm space-y-2">
            <div class="flex justify-between"><dt class="text-slate-400">订单号</dt><dd class="font-mono text-xs text-slate-700">{{ order.id }}</dd></div>
            <div class="flex justify-between"><dt class="text-slate-400">状态</dt><dd class="text-amber-600 font-medium">{{ statusLabel }}</dd></div>
            <div class="flex justify-between"><dt class="text-slate-400">金额</dt><dd class="font-semibold text-slate-800">¥{{ order.amount }}</dd></div>
            <div class="flex justify-between"><dt class="text-slate-400">充值积分</dt><dd class="text-[#ec5b13] font-semibold">{{ order.points }}</dd></div>
            <div class="flex justify-between"><dt class="text-slate-400">下单时间</dt><dd class="text-xs text-slate-600">{{ fmtTime(order.created_at) }}</dd></div>
            <div class="flex justify-between"><dt class="text-slate-400">支付倒计时</dt><dd class="tabular-nums font-medium" :class="remaining > 0 ? 'text-slate-700' : 'text-rose-500'">{{ remaining > 0 ? mmss : '已超时' }}</dd></div>
          </dl>
          <p class="text-[11px] text-slate-400 mt-4 text-center">支付完成后将自动到账,请勿关闭本窗口</p>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.pay-fade-enter-active, .pay-fade-leave-active { transition: opacity 0.2s ease; }
.pay-fade-enter-from, .pay-fade-leave-to { opacity: 0; }
</style>
