import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { LanguageInput } from 'shiki'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitepress'
import justGrammar from './just.tmLanguage.json'
import { githubSlug, plainMarkdown } from './markdown'

const repository = 'https://github.com/stubbedev/shell-lsp'
const docsRoot = path.resolve(fileURLToPath(new URL('..', import.meta.url)))
const repoRoot = path.dirname(docsRoot)

const logo = `data:image/svg+xml,${encodeURIComponent(
  readFileSync(new URL('logo.svg', import.meta.url), 'utf8'),
)}`
const gitHubMark =
  '<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.2.5-2.3 1.3-3.1-.2-.4-.6-1.6 0-3.2 0 0 1-.3 3.4 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8 0 3.2.9.8 1.3 1.9 1.3 3.2 0 4.6-2.8 5.6-5.5 5.9.5.4.9 1 .9 2.2v3.3c0 .3.1.7.8.6A12 12 0 0 0 12 .3"/></svg>'

const sections = [
  {
    text: 'Get started',
    pages: [
      ['Overview', 'overview', 'README.md'],
      ['Editor setup', 'editors', 'docs/editors.md'],
      ['Command line', 'cli', 'docs/cli.md'],
    ],
  },
  {
    text: 'Languages',
    pages: [
      ['Shell scripts', 'shell', 'docs/shell.md'],
      ['Nix', 'nix', 'docs/nix.md'],
      ['Justfiles, Makefiles, Dockerfiles', 'hosts', 'docs/hosts.md'],
    ],
  },
  {
    text: 'Linting and formatting',
    pages: [
      ['Formatting', 'formatting', 'docs/formatting.md'],
      ['Linting', 'linting', 'docs/linting.md'],
      ['All checks', 'checks', 'docs/checks.md'],
      ['Native rules', 'rules', 'docs/rules.md'],
      ['Hadolint compatibility', 'hadolint', 'docs/hadolint.md'],
    ],
  },
  {
    text: 'Reference',
    pages: [
      ['LSP methods', 'lsp-methods', 'docs/lsp-methods.md'],
      ['Corpus checks', 'corpus', 'docs/corpus.md'],
      ['Cross-check', 'crosscheck', 'docs/crosscheck.md'],
    ],
  },
  {
    text: 'Project',
    pages: [
      ['Development', 'development', 'docs/development.md'],
      ['Publishing the extension', 'vscode-publishing', 'docs/vscode-publishing.md'],
      ['How it was built', 'how-it-was-built', 'docs/how-it-was-built.md'],
      ['Clean-room policy', 'clean-room', 'CLEAN_ROOM.md'],
    ],
  },
] as const

const pages = sections.flatMap((section) => section.pages)
const home = 'docs/index.md'
const routes = new Map<string, string>([
  [home, '/'],
  ...pages.map(([, route, file]): [string, string] => [file, `/${route}/`]),
])

const unlisted = readdirSync(docsRoot).filter(
  (name) => name.endsWith('.md') && !routes.has(`docs/${name}`),
)
if (unlisted.length > 0) {
  throw new Error(`docs/.vitepress/config.ts has no sidebar entry for: ${unlisted.join(', ')}`)
}

const srcExclude = [
  'docs/releases/**',
  ...readdirSync(repoRoot, { withFileTypes: true })
    .filter((entry) => entry.name !== 'docs' && !routes.has(entry.name))
    .map((entry) => (entry.isDirectory() ? `${entry.name}/**` : entry.name)),
]

const resolveRootPagesFromDocs: Plugin = {
  name: 'shell-lsp:resolve-root-pages-from-docs',
  enforce: 'pre',
  resolveId(source, importer, options) {
    const isBare = !source.startsWith('.') && !path.isAbsolute(source) && !source.startsWith('\0')
    if (!isBare || importer === undefined) return null
    if (path.dirname(importer.split('?')[0]) !== repoRoot) return null
    return this.resolve(source, path.join(docsRoot, 'index.md'), { ...options, skipSelf: true })
  },
}

export default defineConfig({
  title: 'shell-lsp',
  description: 'One language server for shell and every language that embeds it',
  lang: 'en-US',
  base: '/shell-lsp/',
  srcDir: '..',
  srcExclude,
  rewrites: Object.fromEntries([
    [home, 'index.md'],
    ...pages.map(([, route, file]) => [file, `${route}/index.md`]),
  ]),
  vite: { plugins: [resolveRootPagesFromDocs] },
  cleanUrls: true,
  appearance: 'dark',
  lastUpdated: false,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: logo }],
    ['meta', { name: 'theme-color', content: '#0f131a' }],
  ],
  markdown: {
    attrs: { disable: true },
    anchor: { slugify: githubSlug },
    theme: { light: 'github-light', dark: 'github-dark' },
    languages: ['shellscript', justGrammar as LanguageInput],
    config: (md) => plainMarkdown(md, repoRoot, routes, repository),
  },
  themeConfig: {
    logo,
    nav: [
      { text: 'Docs', link: '/overview/', activeMatch: '^/(?!$)' },
      { text: 'Checks', link: '/checks/' },
      { text: 'Releases', link: `${repository}/releases` },
    ],
    sidebar: sections.map(({ text, pages: items }) => ({
      text,
      items: items.map(([title, route]) => ({ text: title, link: `/${route}/` })),
    })),
    outline: { level: [2, 3] },
    search: { provider: 'local' },
    editLink: {
      pattern: `${repository}/edit/main/:path`,
      text: 'Edit this page on GitHub',
    },
    socialLinks: [{ icon: { svg: gitHubMark }, link: repository, ariaLabel: 'GitHub' }],
    footer: {
      message: `Released under the <a href="${repository}/blob/main/LICENSE">MIT License</a>.`,
      copyright: `<a href="${repository}">Source on GitHub</a> · <a href="${repository}/releases">Releases</a>`,
    },
  },
})
