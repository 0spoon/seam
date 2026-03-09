import { EditorView } from '@codemirror/view';

export interface SlashCommand {
  id: string;
  label: string;
  description: string;
  icon: string;
  action: (view: EditorView) => void;
  keywords?: string[];
}

// Find the slash command text on the current line (from '/' to cursor)
// and return its range { from, to }.
function findSlashRange(view: EditorView): { from: number; to: number } | null {
  const { head } = view.state.selection.main;
  const line = view.state.doc.lineAt(head);
  const lineText = line.text;
  const cursorOffset = head - line.from;

  // Search backwards from cursor for '/' that is at line start or preceded by whitespace
  for (let i = cursorOffset - 1; i >= 0; i--) {
    if (lineText[i] === '/') {
      if (i === 0 || /\s/.test(lineText[i - 1])) {
        return { from: line.from + i, to: head };
      }
      break;
    }
    // Only allow alphanumeric characters between '/' and cursor (the query)
    if (!/[a-zA-Z0-9]/.test(lineText[i])) {
      break;
    }
  }
  return null;
}

export function replaceSlashWithPrefix(view: EditorView, prefix: string): void {
  const range = findSlashRange(view);
  if (!range) return;

  const line = view.state.doc.lineAt(range.from);
  // Replace the slash command text and prepend the prefix to the line content after slash
  view.dispatch({
    changes: { from: line.from, to: range.to, insert: prefix },
    selection: { anchor: line.from + prefix.length },
  });
  view.focus();
}

export function replaceSlashWithText(view: EditorView, text: string): void {
  const range = findSlashRange(view);
  if (!range) return;

  view.dispatch({
    changes: { from: range.from, to: range.to, insert: text },
    selection: { anchor: range.from + text.length },
  });
  view.focus();
}

export function replaceSlashWithBlock(
  view: EditorView,
  before: string,
  after: string,
): void {
  const range = findSlashRange(view);
  if (!range) return;

  const insert = before + after;
  const cursorPos = range.from + before.length;

  view.dispatch({
    changes: { from: range.from, to: range.to, insert },
    selection: { anchor: cursorPos },
  });
  view.focus();
}

export function wrapOrInsert(
  view: EditorView,
  before: string,
  after: string,
  placeholder: string,
): void {
  const range = findSlashRange(view);
  if (!range) return;

  const { from, to } = view.state.selection.main;
  const hasSelection = from !== to && from < range.from;

  if (hasSelection) {
    // Wrap the existing selection and remove the slash text
    const selected = view.state.sliceDoc(from, to);
    const wrapped = before + selected + after;
    // Remove slash command text first, then wrap selection
    view.dispatch({
      changes: [
        { from: range.from, to: range.to, insert: '' },
        { from, to, insert: wrapped },
      ],
    });
  } else {
    // Insert placeholder wrapped in before/after
    const insert = before + placeholder + after;
    view.dispatch({
      changes: { from: range.from, to: range.to, insert },
      selection: {
        anchor: range.from + before.length,
        head: range.from + before.length + placeholder.length,
      },
    });
  }
  view.focus();
}

export const slashCommands: SlashCommand[] = [
  {
    id: 'h1',
    label: 'Heading 1',
    description: 'Large section heading',
    icon: 'Heading1',
    action: (view) => replaceSlashWithPrefix(view, '# '),
    keywords: ['title', 'header'],
  },
  {
    id: 'h2',
    label: 'Heading 2',
    description: 'Medium section heading',
    icon: 'Heading2',
    action: (view) => replaceSlashWithPrefix(view, '## '),
    keywords: ['title', 'header'],
  },
  {
    id: 'h3',
    label: 'Heading 3',
    description: 'Small section heading',
    icon: 'Heading3',
    action: (view) => replaceSlashWithPrefix(view, '### '),
    keywords: ['title', 'header'],
  },
  {
    id: 'bold',
    label: 'Bold',
    description: 'Bold text',
    icon: 'Bold',
    action: (view) => wrapOrInsert(view, '**', '**', 'bold'),
    keywords: ['strong'],
  },
  {
    id: 'italic',
    label: 'Italic',
    description: 'Italic text',
    icon: 'Italic',
    action: (view) => wrapOrInsert(view, '*', '*', 'italic'),
    keywords: ['emphasis', 'em'],
  },
  {
    id: 'code',
    label: 'Code Block',
    description: 'Fenced code block',
    icon: 'Code',
    action: (view) => replaceSlashWithBlock(view, '```\n', '\n```'),
    keywords: ['fence', 'snippet'],
  },
  {
    id: 'list',
    label: 'Bullet List',
    description: 'Unordered list item',
    icon: 'List',
    action: (view) => replaceSlashWithPrefix(view, '- '),
    keywords: ['unordered', 'ul'],
  },
  {
    id: 'checklist',
    label: 'Checklist',
    description: 'Checkbox list item',
    icon: 'ListChecks',
    action: (view) => replaceSlashWithPrefix(view, '- [ ] '),
    keywords: ['todo', 'task', 'checkbox'],
  },
  {
    id: 'link',
    label: 'Wikilink',
    description: 'Link to another note',
    icon: 'Link2',
    action: (view) => replaceSlashWithText(view, '[['),
    keywords: ['reference', 'note'],
  },
  {
    id: 'divider',
    label: 'Divider',
    description: 'Horizontal rule',
    icon: 'Minus',
    action: (view) => replaceSlashWithBlock(view, '---\n', ''),
    keywords: ['hr', 'separator', 'line'],
  },
  {
    id: 'date',
    label: 'Date',
    description: "Insert today's date",
    icon: 'Calendar',
    action: (view) =>
      replaceSlashWithText(view, new Date().toISOString().split('T')[0]),
    keywords: ['today'],
  },
  {
    id: 'time',
    label: 'Time',
    description: 'Insert current time',
    icon: 'Clock',
    action: (view) =>
      replaceSlashWithText(
        view,
        new Date().toLocaleTimeString('en-US', {
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        }),
      ),
    keywords: ['now', 'timestamp'],
  },
  {
    id: 'quote',
    label: 'Blockquote',
    description: 'Quoted text',
    icon: 'Quote',
    action: (view) => replaceSlashWithPrefix(view, '> '),
    keywords: ['citation'],
  },
  {
    id: 'table',
    label: 'Table',
    description: 'Markdown table',
    icon: 'Table',
    action: (view) =>
      replaceSlashWithBlock(
        view,
        '| Column 1 | Column 2 | Column 3 |\n| --- | --- | --- |\n| ',
        ' | | |',
      ),
    keywords: ['grid', 'columns'],
  },
];
