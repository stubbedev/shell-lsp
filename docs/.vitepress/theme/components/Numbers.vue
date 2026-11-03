<script setup lang="ts">
import { useData } from 'vitepress'

const { frontmatter } = useData()
</script>

<template>
  <section class="numbers">
    <dl>
      <div v-for="number in frontmatter.numbers" :key="number.value">
        <dt>{{ number.value }}</dt>
        <dd>{{ number.label }}</dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.numbers {
  max-width: 1152px;
  padding: 104px 24px 96px;
  margin: 0 auto;
}
dl {
  display: grid;
  overflow: hidden;
  margin: 0;
  background: var(--vp-c-divider);
  border: 1px solid var(--vp-c-divider);
  border-radius: 14px;
  gap: 1px;
}
div {
  padding: 28px;
  background: var(--vp-c-bg-soft);
}
dt {
  font-family: var(--vp-font-family-mono);
  font-size: 34px;
  font-weight: 600;
  line-height: 42px;
  letter-spacing: -0.02em;
  color: var(--vp-c-brand-1);
}
dd {
  margin: 8px 0 0;
  font-size: 14px;
  line-height: 22px;
  color: var(--vp-c-text-2);
}
@media (min-width: 768px) {
  dl {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
</style>
