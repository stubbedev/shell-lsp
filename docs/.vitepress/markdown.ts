import path from 'node:path'
import type { MarkdownRenderer } from 'vitepress'

type RenderRule = NonNullable<MarkdownRenderer['renderer']['rules']['text']>

const htmlTags = new Set(
  'a abbr b blockquote br code dd del details div dl dt em h1 h2 h3 h4 h5 h6 hr i img ins kbd li mark ol p pre q s samp small span strong sub summary sup table tbody td tfoot th thead tr tt u ul var'.split(
    ' ',
  ),
)
const anyTag = /<\/?([a-zA-Z][\w:.-]*)(?:\s[^<>]*)?\/?>/g
const urlScheme = /^[a-z][a-z0-9+.-]*:/i

const literalBraces = (html: string) => html.replaceAll('{{', '&#123;&#123;')

function literalHtml(html: string): string {
  const known = html
    .replace(anyTag, (tag, name: string) =>
      htmlTags.has(name.toLowerCase()) ? tag : tag.replaceAll('<', '&lt;').replaceAll('>', '&gt;'),
    )
    .replaceAll('<a name=', '<a id=')
  return literalBraces(known)
}

export const githubSlug = (heading: string) =>
  heading
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}_\- ]/gu, '')
    .replaceAll(' ', '-')

function siteHref(
  fromFile: string,
  href: string,
  routes: ReadonlyMap<string, string>,
  repository: string,
): string {
  if (urlScheme.test(href) || href.startsWith('#') || href.startsWith('/')) return href
  const hashAt = href.indexOf('#')
  const file = hashAt < 0 ? href : href.slice(0, hashAt)
  const hash = hashAt < 0 ? '' : href.slice(hashAt)
  const target = path.posix
    .normalize(path.posix.join(path.posix.dirname(fromFile), file))
    .replace(/\/$/, '')
  const route = routes.get(target)
  return route === undefined ? `${repository}/blob/main/${target}${hash}` : `${route}${hash}`
}

function ruleOf(md: MarkdownRenderer, name: string): RenderRule {
  const rule = md.renderer.rules[name]
  if (!rule) throw new Error(`the Markdown renderer has no ${name} rule to wrap`)
  return rule
}

function htmlRule(md: MarkdownRenderer, name: string): RenderRule {
  const inner = ruleOf(md, name)
  return (tokens, idx, options, env, self) => {
    tokens[idx].content = literalHtml(tokens[idx].content)
    return inner(tokens, idx, options, env, self)
  }
}

function bracesRule(md: MarkdownRenderer, name: string): RenderRule {
  const inner = ruleOf(md, name)
  return (tokens, idx, options, env, self) => literalBraces(inner(tokens, idx, options, env, self))
}

export function plainMarkdown(
  md: MarkdownRenderer,
  repoRoot: string,
  routes: ReadonlyMap<string, string>,
  repository: string,
): void {
  md.renderer.rules.text = bracesRule(md, 'text')
  md.renderer.rules.code_inline = bracesRule(md, 'code_inline')
  md.renderer.rules.html_block = htmlRule(md, 'html_block')
  md.renderer.rules.html_inline = htmlRule(md, 'html_inline')

  const fence = ruleOf(md, 'fence')
  md.renderer.rules.fence = (tokens, idx, options, env, self) =>
    tokens[idx].info.trim() === 'mermaid'
      ? `<Mermaid source="${encodeURIComponent(tokens[idx].content)}" />`
      : fence(tokens, idx, options, env, self)

  md.core.ruler.push('repository_links', (state) => {
    const source: string | undefined = state.env.realPath ?? state.env.path
    if (source === undefined) return
    const fromFile = path.relative(repoRoot, source).split(path.sep).join('/')
    for (const block of state.tokens) {
      for (const token of block.children ?? []) {
        if (token.type !== 'link_open') continue
        const href = token.attrGet('href')
        if (href !== null) token.attrSet('href', siteHref(fromFile, href, routes, repository))
      }
    }
  })
}
