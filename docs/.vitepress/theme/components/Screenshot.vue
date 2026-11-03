<script setup lang="ts">
const props = defineProps<{ file: string; alt: string; eager?: boolean }>()

const urls = import.meta.glob<string>('../../../assets/screenshots/*.png', {
  eager: true,
  query: '?url',
  import: 'default',
})
const url = urls[`../../../assets/screenshots/${props.file}`]
if (!url) throw new Error(`docs/assets/screenshots has no ${props.file}`)
</script>

<template>
  <figure class="screenshot">
    <div class="bar" aria-hidden="true"><i /><i /><i /></div>
    <a :href="url" target="_blank" rel="noopener">
      <img :src="url" :alt="alt" :loading="eager ? 'eager' : 'lazy'" />
    </a>
  </figure>
</template>

<style scoped>
.screenshot {
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--sl-frame-border);
  border-radius: 12px;
  background: #1e1e1e;
  box-shadow: var(--sl-frame-shadow);
}
.bar {
  display: flex;
  gap: 7px;
  align-items: center;
  height: 30px;
  padding: 0 14px;
  background: #181b20;
}
.bar i {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #3a404b;
}
a {
  display: block;
}
img {
  display: block;
  width: 100%;
  height: auto;
}
</style>
