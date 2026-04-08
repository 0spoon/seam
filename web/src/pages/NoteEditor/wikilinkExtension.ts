import {
  ViewPlugin,
  Decoration,
  type DecorationSet,
  EditorView,
  type ViewUpdate,
  MatchDecorator,
} from '@codemirror/view';
import {
  autocompletion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete';
import { searchFTS } from '../../api/client';

// --- Wikilink Decoration ---

// Matches [[target]] and [[target|display]], highlighting the wikilink with
// amber color and dimmed delimiters per FE_DESIGN.md Section 6.9.

const wikilinkMatcher = new MatchDecorator({
  regexp: /\[\[([^\]]+)\]\]/g,
  decoration: () =>
    Decoration.mark({
      class: 'cm-wikilink',
    }),
});

export const wikilinkDecorationPlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = wikilinkMatcher.createDeco(view);
    }

    update(update: ViewUpdate) {
      this.decorations = wikilinkMatcher.updateDeco(update, this.decorations);
    }
  },
  {
    decorations: (v) => v.decorations,
  },
);

// CSS styles for wikilink decoration are injected via a theme extension.
export const wikilinkDecorationTheme = EditorView.baseTheme({
  '.cm-wikilink': {
    color: 'var(--accent-primary)',
    textDecorationLine: 'underline',
    textDecorationStyle: 'dotted',
    textUnderlineOffset: '2px',
  },
});

// --- Wikilink Autocomplete ---

// When the user types "[[", fetch note titles and show a completion dropdown.
// Completions are fetched from the search API.

let cachedTitles: { label: string; noteId: string }[] = [];

async function fetchNoteTitles(query: string): Promise<{ label: string; noteId: string }[]> {
  try {
    // Use search to find matching notes.
    const { results } = await searchFTS(query || '*', 10, 0);
    return results.map((r) => ({
      label: r.title,
      noteId: r.note_id,
    }));
  } catch {
    return cachedTitles;
  }
}

async function wikilinkCompletions(context: CompletionContext): Promise<CompletionResult | null> {
  // Look for "[[" before the cursor.
  const beforeCursor = context.state.sliceDoc(Math.max(0, context.pos - 100), context.pos);
  const match = beforeCursor.match(/\[\[([^\]]*?)$/);
  if (!match) return null;

  const query = match[1];
  const from = context.pos - query.length;

  // Fetch titles matching the query.
  const titles = await fetchNoteTitles(query);
  cachedTitles = titles;

  const filtered = query
    ? titles.filter((t) => t.label.toLowerCase().includes(query.toLowerCase()))
    : titles;

  if (filtered.length === 0) return null;

  return {
    from,
    options: filtered.map((t) => ({
      label: t.label,
      apply: `${t.label}]]`,
      type: 'text',
    })),
    filter: false,
  };
}

export const wikilinkAutocomplete = autocompletion({
  override: [wikilinkCompletions],
  activateOnTyping: true,
});
