import { h } from 'vue'
import { inBrowser, withBase } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import HeroShot from './components/HeroShot.vue'
import Languages from './components/Languages.vue'
import Mermaid from './components/Mermaid.vue'
import Numbers from './components/Numbers.vue'
import Showcase from './components/Showcase.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  Layout: () =>
    h(DefaultTheme.Layout, null, {
      'home-hero-actions-after': () => h(Languages),
      'home-hero-after': () => h(HeroShot),
      'home-features-after': () => [h(Showcase), h(Numbers)],
    }),
  enhanceApp({ app }) {
    app.component('Mermaid', Mermaid)
    if (inBrowser && location.pathname === withBase('/') && location.hash) {
      location.replace(withBase(`/overview/${location.hash}`))
    }
  },
} satisfies Theme
