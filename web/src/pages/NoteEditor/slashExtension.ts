import { StateField, StateEffect, type Transaction } from '@codemirror/state';
import { EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view';

export interface SlashMenuState {
  active: boolean;
  query: string;
  from: number;
  pos: { top: number; left: number } | null;
}

const defaultState: SlashMenuState = {
  active: false,
  query: '',
  from: 0,
  pos: null,
};

export const slashMenuEffect = StateEffect.define<SlashMenuState>();

export const slashMenuField = StateField.define<SlashMenuState>({
  create() {
    return defaultState;
  },
  update(value: SlashMenuState, tr: Transaction) {
    for (const e of tr.effects) {
      if (e.is(slashMenuEffect)) {
        return e.value;
      }
    }
    // If the document changed and we are active, recompute the query
    if (value.active && tr.docChanged) {
      const { head } = tr.state.selection.main;
      const line = tr.state.doc.lineAt(head);
      const lineText = line.text;
      const cursorOffset = head - line.from;

      // Find the '/' in the current line that starts our slash command
      const slashOffset = cursorOffset > 0 ? findSlashOffset(lineText, cursorOffset) : -1;

      if (slashOffset < 0) {
        return defaultState;
      }

      const query = lineText.slice(slashOffset + 1, cursorOffset);
      return { ...value, query, from: line.from + slashOffset };
    }
    // If selection changed without doc change, check if we moved away
    if (value.active && tr.selection) {
      const { head } = tr.state.selection.main;
      const line = tr.state.doc.lineAt(head);
      const cursorOffset = head - line.from;
      const slashOffset = findSlashOffset(line.text, cursorOffset);

      if (slashOffset < 0 || line.from + slashOffset !== value.from) {
        return defaultState;
      }
    }
    return value;
  },
});

// Find the offset of '/' in lineText looking backwards from cursorOffset.
// Returns -1 if not found or invalid context.
function findSlashOffset(lineText: string, cursorOffset: number): number {
  for (let i = cursorOffset - 1; i >= 0; i--) {
    if (lineText[i] === '/') {
      // '/' must be at line start or preceded by whitespace
      if (i === 0 || /\s/.test(lineText[i - 1])) {
        return i;
      }
      return -1;
    }
    // Only alphanumeric chars allowed in the query portion
    if (!/[a-zA-Z0-9]/.test(lineText[i])) {
      return -1;
    }
  }
  return -1;
}

// ViewPlugin that detects '/' trigger and manages slash menu lifecycle.
const slashMenuPlugin = ViewPlugin.fromClass(
  class {
    constructor() {}

    update(update: ViewUpdate) {
      const state = update.state.field(slashMenuField);

      // If already active, the StateField handles query tracking.
      // We only need to detect initial activation here.
      if (state.active) return;

      // Check if user just typed '/'
      if (!update.docChanged) return;

      const { head } = update.state.selection.main;
      const line = update.state.doc.lineAt(head);
      const cursorOffset = head - line.from;

      if (cursorOffset <= 0) return;

      const charBefore = line.text[cursorOffset - 1];
      if (charBefore !== '/') return;

      // '/' must be at line start or preceded by whitespace
      const slashOffset = cursorOffset - 1;
      if (slashOffset > 0 && !/\s/.test(line.text[slashOffset - 1])) return;

      // Get screen coordinates for the menu position
      const coords = update.view.coordsAtPos(head);
      if (!coords) return;

      const editorRect = update.view.dom.getBoundingClientRect();
      const pos = {
        top: coords.bottom - editorRect.top,
        left: coords.left - editorRect.left,
      };

      update.view.dispatch({
        effects: slashMenuEffect.of({
          active: true,
          query: '',
          from: line.from + slashOffset,
          pos,
        }),
      });
    }
  },
);

export function dismissSlashMenu(view: EditorView): void {
  view.dispatch({
    effects: slashMenuEffect.of(defaultState),
  });
}

// Create the CodeMirror extension array for slash command support.
// The onStateChange callback fires whenever the slash menu state changes,
// letting the React component re-render the floating menu.
export function createSlashExtension(onStateChange: (state: SlashMenuState) => void) {
  const notifyPlugin = ViewPlugin.fromClass(
    class {
      prev: SlashMenuState;

      constructor(view: EditorView) {
        this.prev = view.state.field(slashMenuField);
      }

      update(update: ViewUpdate) {
        const next = update.state.field(slashMenuField);
        if (
          next.active !== this.prev.active ||
          next.query !== this.prev.query ||
          next.from !== this.prev.from
        ) {
          // Update screen coordinates when query changes (cursor may have moved)
          let pos = next.pos;
          if (next.active) {
            const coords = update.view.coordsAtPos(next.from);
            if (coords) {
              const editorRect = update.view.dom.getBoundingClientRect();
              pos = {
                top: coords.bottom - editorRect.top,
                left: coords.left - editorRect.left,
              };
            }
          }
          this.prev = { ...next, pos };
          onStateChange(this.prev);
        }
      }
    },
  );

  return [slashMenuField, slashMenuPlugin, notifyPlugin];
}
