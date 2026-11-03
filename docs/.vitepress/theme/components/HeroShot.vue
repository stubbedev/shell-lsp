<script setup lang="ts">
import { useData } from 'vitepress'
import Screenshot from './Screenshot.vue'

const { frontmatter } = useData()
</script>

<template>
  <div class="hero-shot">
    <Screenshot :file="frontmatter.screenshot.file" :alt="frontmatter.screenshot.alt" eager />
    <p>{{ frontmatter.screenshot.caption }}</p>
  </div>
</template>

<style scoped>
.hero-shot {
  position: relative;
  max-width: 1040px;
  margin: 0 auto;
  padding: 0 24px 72px;
}
.hero-shot::before {
  position: absolute;
  inset: 8% 6% 22%;
  z-index: -1;
  content: '';
  background: var(--sl-glow);
  filter: blur(70px);
}
p {
  margin-top: 16px;
  font-size: 14px;
  line-height: 22px;
  color: var(--vp-c-text-2);
  text-align: center;
}
</style>
