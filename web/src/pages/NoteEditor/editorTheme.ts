import { EditorView } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';

const editorTheme = EditorView.theme({
  '&': {
    backgroundColor: 'var(--bg-base)',
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 'var(--font-size-base)',
    height: '100%',
  },
  '.cm-content': {
    caretColor: 'var(--accent-primary)',
    padding: '16px 0',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: 'var(--accent-primary)',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, &.cm-focused .cm-selectionBackground .cm-selectionBackground':
    {
      backgroundColor: 'var(--accent-muted)',
    },
  '.cm-activeLine': {
    backgroundColor: 'rgba(22, 25, 34, 0.5)',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--bg-base)',
    color: 'var(--text-tertiary)',
    border: 'none',
    width: '48px',
  },
  '.cm-activeLineGutter': {
    color: 'var(--text-secondary)',
    backgroundColor: 'transparent',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    textAlign: 'right',
    paddingRight: '8px',
  },
  '&.cm-focused': {
    outline: 'none',
  },
  '.cm-matchingBracket': {
    color: 'var(--accent-primary)',
    textDecoration: 'underline',
  },
  '.cm-scroller': {
    overflow: 'auto',
  },
});

const highlightStyle = HighlightStyle.define([
  { tag: tags.heading, color: 'var(--accent-primary)', fontWeight: '600' },
  { tag: tags.heading1, fontSize: '1.4em' },
  { tag: tags.heading2, fontSize: '1.2em' },
  { tag: tags.heading3, fontSize: '1.1em' },
  { tag: tags.strong, color: 'var(--text-primary)', fontWeight: '600' },
  { tag: tags.emphasis, color: 'var(--text-primary)', fontStyle: 'italic' },
  { tag: tags.link, color: 'var(--accent-primary)' },
  { tag: tags.url, color: 'var(--text-tertiary)' },
  { tag: tags.monospace, color: 'var(--accent-secondary)' },
  { tag: tags.processingInstruction, color: 'var(--text-tertiary)' },
  { tag: tags.quote, color: 'var(--text-secondary)', fontStyle: 'italic' },
  { tag: tags.list, color: 'var(--text-tertiary)' },
  { tag: tags.meta, color: 'var(--text-tertiary)' },
]);

export const seamEditorTheme = [editorTheme, syntaxHighlighting(highlightStyle)];
