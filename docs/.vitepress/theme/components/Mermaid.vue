<script setup lang="ts">
import { onMounted, ref, useId, watch } from 'vue'
import { useData } from 'vitepress'

const moduleUrl = 'https://cdn.jsdelivr.net/npm/mermaid@12.2.0/dist/mermaid.esm.min.mjs'

const props = defineProps<{ source: string }>()
const { isDark } = useData()
const code = decodeURIComponent(props.source)
const id = `mermaid-${useId()}`
const svg = ref('')

async function draw(): Promise<void> {
  const { default: mermaid } = await import(/* @vite-ignore */ moduleUrl)
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'base',
    themeVariables: isDark.value
      ? {
          darkMode: true,
          background: '#151a22',
          primaryColor: '#1c232e',
          primaryBorderColor: '#4fd1b5',
          primaryTextColor: '#e6eaf0',
          lineColor: '#7d8896',
          edgeLabelBackground: '#151a22',
        }
      : {
          background: '#f4f6f8',
          primaryColor: '#e6f6f2',
          primaryBorderColor: '#0b7363',
          primaryTextColor: '#1c2333',
          lineColor: '#6b7480',
          edgeLabelBackground: '#f4f6f8',
        },
  })
  svg.value = (await mermaid.render(id, code)).svg
}

onMounted(draw)
watch(isDark, draw)
</script>

<template>
  <div v-if="svg" class="mermaid" v-html="svg" />
  <pre v-else class="mermaid-source">{{ code }}</pre>
</template>

<style scoped>
.mermaid,
.mermaid-source {
  overflow-x: auto;
  padding: 20px;
  margin: 16px 0;
  background: var(--vp-c-bg-soft);
  border-radius: 8px;
}
.mermaid {
  display: flex;
  justify-content: center;
}
.mermaid :deep(p) {
  margin: 0;
  line-height: 1.5;
}
.mermaid-source {
  font-size: 13px;
}
</style>
