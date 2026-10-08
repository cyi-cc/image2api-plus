<script setup>
import { ref } from 'vue'
import Icon from './Icon.vue'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  presets: { type: Array, default: () => [] },
  placeholder: { type: String, default: '输入后回车添加' },
})
const emit = defineEmits(['update:modelValue'])

const input = ref('')

function add(v) {
  const s = String(v ?? '').trim()
  if (s && !props.modelValue.includes(s)) emit('update:modelValue', [...props.modelValue, s])
}
function addInput() { add(input.value); input.value = '' }
function remove(i) {
  const arr = [...props.modelValue]
  arr.splice(i, 1)
  emit('update:modelValue', arr)
}
</script>

<template>
  <div>
    <div v-if="modelValue.length" class="flex flex-wrap gap-1.5 mb-2">
      <span v-for="(t, i) in modelValue" :key="t" class="chip">
        {{ t }}
        <button type="button" @click="remove(i)" class="chip-x" :title="`移除 ${t}`">
          <Icon name="close" class="w-3 h-3" />
        </button>
      </span>
    </div>
    <div class="flex gap-2">
      <input v-model="input" @keyup.enter.prevent="addInput" class="field" :placeholder="placeholder" />
      <button type="button" @click="addInput" class="btn-soft shrink-0">添加</button>
    </div>
    <div v-if="presets.length" class="flex flex-wrap gap-1.5 mt-2">
      <button v-for="p in presets" :key="p" type="button" @click="add(p)" :disabled="modelValue.includes(p)"
              class="preset-btn">
        + {{ p }}
      </button>
    </div>
  </div>
</template>

<style scoped>
/* Tag chip — small pill that reads as a brand-tinted token on the dark
   admin shell. The close button stays subtle until you hover, then turns
   white on a muted-rose hover state so removal feels intentional. */
.chip {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  padding: 0.25rem 0.55rem 0.25rem 0.65rem;
  font-size: 0.75rem;
  font-weight: 500;
  border-radius: 9999px;
  color: rgb(255 237 213);                     /* near-white orange */
  background: linear-gradient(135deg, rgb(253 186 116 / 0.18), rgb(251 113 133 / 0.14));
  box-shadow: inset 0 0 0 1px rgb(253 186 116 / 0.3);
  transition: background 0.15s ease;
}
.chip:hover { background: linear-gradient(135deg, rgb(253 186 116 / 0.25), rgb(251 113 133 / 0.2)); }

.chip-x {
  display: grid;
  place-items: center;
  width: 1.1rem;
  height: 1.1rem;
  margin-right: -0.15rem;
  border-radius: 9999px;
  color: rgb(255 255 255 / 0.55);
  transition: color 0.12s ease, background 0.12s ease;
}
.chip-x:hover {
  color: white;
  background: rgb(244 63 94 / 0.35);            /* rose hint on hover */
}

/* Preset suggestion buttons — sit below the input, soft dark surface. */
.preset-btn {
  font-size: 0.72rem;
  padding: 0.25rem 0.55rem;
  border-radius: 0.5rem;
  color: rgb(255 255 255 / 0.6);
  background: rgb(255 255 255 / 0.04);
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / 0.08);
  transition: background 0.15s ease, color 0.15s ease;
}
.preset-btn:hover:not(:disabled) { background: rgb(255 255 255 / 0.08); color: white; }
.preset-btn:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
