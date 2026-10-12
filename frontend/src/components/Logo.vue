<script setup>
// Vivid mark: an open spectrum ring (lens/aperture) around a play triangle,
// with a shutter dot closing the gap — image + video in one glyph.
// Transparent background so it reads on both the dark and light shells.
// Colors follow the aurora palette (violet / pink / cyan).
// Each instance gets unique gradient ids (useId) so multiple logos on one
// page don't share/clobber defs.
import { useId } from 'vue'

defineProps({ size: { type: [Number, String], default: 40 } })

const uid = useId()
const gRing = `vring-${uid}`
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 48 48" fill="none"
       xmlns="http://www.w3.org/2000/svg" class="shrink-0">
    <defs>
      <linearGradient :id="gRing" x1="6" y1="6" x2="42" y2="42" gradientUnits="userSpaceOnUse">
        <stop stop-color="#7c5cfc" />
        <stop offset="0.5" stop-color="#e879f9" />
        <stop offset="1" stop-color="#22d3ee" />
      </linearGradient>
    </defs>

    <g transform="translate(24 24)">
      <!-- open ring: dash gap leaves room for the shutter dot -->
      <circle r="17.5" fill="none" :stroke="`url(#${gRing})`" stroke-width="4.4"
              stroke-linecap="round" stroke-dasharray="84 32" transform="rotate(-58)" />
      <!-- play triangle = image + video -->
      <path d="M-3 -6.5 L8.5 0 L-3 6.5 Z" fill="#7c5cfc" stroke="#111" stroke-width="1.4"
            stroke-linejoin="round" />
      <!-- shutter dot -->
      <circle cx="13" cy="-13" r="4.2" fill="#22d3ee" stroke="#111" stroke-width="1.6" />
    </g>
  </svg>
</template>
