<script setup>
// Default mark: an ink squircle holding a soft white sparkle (the "idea"),
// a small companion star and a warm dot — cute, and on-theme with the
// ink-black / warm-paper palette used across the site.
// Each instance gets unique gradient ids (useId) so multiple logos on one
// page don't share/clobber defs.
import { useId } from 'vue'

defineProps({ size: { type: [Number, String], default: 40 } })

const uid = useId()
const gBg = `vbg-${uid}`
const gShine = `vshine-${uid}`
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 48 48" fill="none"
       xmlns="http://www.w3.org/2000/svg" class="shrink-0">
    <defs>
      <linearGradient :id="gBg" x1="8" y1="4" x2="40" y2="44" gradientUnits="userSpaceOnUse">
        <stop stop-color="#3f3f46" />
        <stop offset="1" stop-color="#18181b" />
      </linearGradient>
      <linearGradient :id="gShine" x1="24" y1="3" x2="24" y2="24" gradientUnits="userSpaceOnUse">
        <stop stop-color="#fff" stop-opacity="0.14" />
        <stop offset="0.9" stop-color="#fff" stop-opacity="0" />
      </linearGradient>
    </defs>

    <!-- squircle tile -->
    <rect x="2" y="2" width="44" height="44" rx="15" :fill="`url(#${gBg})`" />
    <rect x="2" y="2" width="44" height="22" rx="15" :fill="`url(#${gShine})`" />

    <!-- big sparkle -->
    <path d="M22 11 Q23.4 22.6 35 24 Q23.4 25.4 22 37 Q20.6 25.4 9 24 Q20.6 22.6 22 11 Z"
          fill="#fff" stroke="#fff" stroke-width="2.2" stroke-linejoin="round" />
    <!-- small companion star -->
    <path d="M35.5 9 Q36.2 13.8 41 14.5 Q36.2 15.2 35.5 20 Q34.8 15.2 30 14.5 Q34.8 13.8 35.5 9 Z"
          fill="#fff" stroke="#fff" stroke-width="1.4" stroke-linejoin="round" opacity="0.92" />
    <!-- warm dot -->
    <circle cx="36" cy="36" r="2.6" fill="#fb923c" />
  </svg>
</template>
