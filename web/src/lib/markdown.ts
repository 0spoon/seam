import MarkdownIt from 'markdown-it';

const md = new MarkdownIt({
  html: false,
  linkify: true,
  typographer: true,
});

// Add target="_blank" and rel="noopener noreferrer" to auto-linked URLs
// to prevent navigation away from the SPA and for security.
const defaultLinkOpen =
  md.renderer.rules.link_open ||
  ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));

md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  const token = tokens[idx];
  // Only modify auto-linked URLs (not wikilinks, which use data-wikilink).
  const wikilinkAttr = token.attrGet('data-wikilink');
  if (!wikilinkAttr) {
    token.attrSet('target', '_blank');
    token.attrSet('rel', 'noopener noreferrer');
  }
  return defaultLinkOpen(tokens, idx, options, env, self);
};

// Wikilink plugin: transform [[target]] and [[target|display]] into links
md.inline.ruler.push('wikilink', (state, silent) => {
  const src = state.src;
  const pos = state.pos;

  if (src[pos] !== '[' || src[pos + 1] !== '[') return false;

  const closePos = src.indexOf(']]', pos + 2);
  if (closePos === -1) return false;

  if (!silent) {
    const content = src.slice(pos + 2, closePos);
    const parts = content.split('|');
    const target = parts[0].trim();
    const display = parts.length > 1 ? parts[1].trim() : target;

    const token = state.push('wikilink', '', 0);
    token.content = display;
    token.meta = { target };
  }

  state.pos = closePos + 2;
  return true;
});

md.renderer.rules['wikilink'] = (tokens, idx) => {
  const token = tokens[idx];
  const target = (token.meta as { target: string }).target;
  const display = token.content;
  const escaped = target.replace(/&/g, '&amp;').replace(/"/g, '&quot;');
  return `<a class="wikilink" data-wikilink="${escaped}" href="#">${md.utils.escapeHtml(display)}</a>`;
};

export function renderMarkdown(source: string): string {
  return md.render(source);
}

export { md };
