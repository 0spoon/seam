import DOMPurify from 'dompurify';

// Configure DOMPurify to reject javascript: URLs in links.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    const href = node.getAttribute('href') || '';
    if (href.startsWith('javascript:')) {
      node.removeAttribute('href');
    }
  }
});

export function sanitizeHtml(html: string): string {
  return DOMPurify.sanitize(html);
}
