import { describe, it, expect } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  it('renders basic markdown', () => {
    const html = renderMarkdown('# Hello\n\nThis is **bold** text.');
    expect(html).toContain('<h1>Hello</h1>');
    expect(html).toContain('<strong>bold</strong>');
  });

  it('renders wikilinks as clickable links', () => {
    const html = renderMarkdown('Link to [[Some Note]]');
    expect(html).toContain('class="wikilink"');
    expect(html).toContain('data-wikilink="Some Note"');
    expect(html).toContain('>Some Note</a>');
  });

  it('renders wikilinks with display aliases', () => {
    const html = renderMarkdown('See [[Target Note|display text]]');
    expect(html).toContain('data-wikilink="Target Note"');
    expect(html).toContain('>display text</a>');
  });

  it('renders multiple wikilinks', () => {
    const html = renderMarkdown('[[First]] and [[Second]]');
    expect(html).toContain('data-wikilink="First"');
    expect(html).toContain('data-wikilink="Second"');
  });

  it('renders inline code', () => {
    const html = renderMarkdown('Use `console.log()`');
    expect(html).toContain('<code>console.log()</code>');
  });

  it('renders paragraphs', () => {
    const html = renderMarkdown('First paragraph.\n\nSecond paragraph.');
    expect(html).toContain('<p>First paragraph.</p>');
    expect(html).toContain('<p>Second paragraph.</p>');
  });

  it('renders links', () => {
    const html = renderMarkdown('[text](https://example.com)');
    expect(html).toContain('href="https://example.com"');
    expect(html).toContain('>text</a>');
  });
});
