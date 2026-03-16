import DOMPurify from 'dompurify';

// Configure DOMPurify to reject javascript: URLs in links.
// Case-insensitive check with whitespace trimming for defense-in-depth.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    const href = node.getAttribute('href') || '';
    if (href.toLowerCase().trimStart().startsWith('javascript:')) {
      node.removeAttribute('href');
    }
  }
});

export function sanitizeHtml(html: string): string {
  return DOMPurify.sanitize(html);
}
