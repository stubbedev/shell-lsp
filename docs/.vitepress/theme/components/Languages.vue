<script setup lang="ts">
import { useData } from 'vitepress'

const { frontmatter } = useData()
</script>

<template>
  <ul class="languages">
    <li v-for="language in frontmatter.languages" :key="language">{{ language }}</li>
  </ul>
</template>

<style scoped>
.languages {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  padding: 0;
  margin: 32px 0 0;
  list-style: none;
}
li {
  padding: 3px 11px;
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  line-height: 20px;
  color: var(--vp-c-text-2);
  background: var(--vp-c-bg-soft);
  border: 1px solid var(--vp-c-divider);
  border-radius: 999px;
}
</style>
