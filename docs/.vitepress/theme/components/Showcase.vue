<script setup lang="ts">
import { useData, withBase } from 'vitepress'
import Screenshot from './Screenshot.vue'

const { frontmatter } = useData()
</script>

<template>
  <section class="showcase">
    <article v-for="item in frontmatter.showcase" :key="item.file" class="row">
      <div class="copy">
        <h2>{{ item.title }}</h2>
        <p>{{ item.text }}</p>
        <a :href="withBase(item.link)">{{ item.linkText }} <span aria-hidden="true">→</span></a>
      </div>
      <Screenshot :file="item.file" :alt="item.alt" />
    </article>
  </section>
</template>

<style scoped>
.showcase {
  display: grid;
  gap: 72px;
  max-width: 1152px;
  padding: 88px 24px 0;
  margin: 0 auto;
}
.row {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 24px;
}
h2 {
  font-size: 26px;
  font-weight: 650;
  line-height: 34px;
  letter-spacing: -0.015em;
}
p {
  margin-top: 12px;
  font-size: 16px;
  line-height: 26px;
  color: var(--vp-c-text-2);
}
a {
  display: inline-block;
  margin-top: 14px;
  font-size: 14px;
  font-weight: 500;
  color: var(--vp-c-brand-1);
}
a:hover {
  color: var(--vp-c-brand-2);
}
@media (min-width: 960px) {
  .showcase {
    padding-top: 120px;
    gap: 104px;
  }
  .row {
    grid-template-columns: minmax(0, 5fr) minmax(0, 8fr);
    gap: 56px;
    align-items: center;
  }
  .row:nth-child(even) {
    grid-template-columns: minmax(0, 8fr) minmax(0, 5fr);
  }
  .row:nth-child(even) .copy {
    order: 2;
  }
}
</style>
