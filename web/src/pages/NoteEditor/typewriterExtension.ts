import { ViewPlugin, type ViewUpdate } from '@codemirror/view';

// Typewriter scrolling keeps the cursor line vertically centered in
// the editor viewport. Intended for use in zen/focus mode.
export const typewriterScrolling = ViewPlugin.fromClass(class {
  update(update: ViewUpdate) {
    if (update.selectionSet || update.docChanged) {
      const view = update.view;
      const head = view.state.selection.main.head;
      const coords = view.coordsAtPos(head);
      if (coords) {
        const viewportHeight = view.dom.clientHeight;
        const targetY = viewportHeight / 2;
        const currentY = coords.top - view.dom.getBoundingClientRect().top;
        if (Math.abs(currentY - targetY) > 20) {
          view.scrollDOM.scrollTop += currentY - targetY;
        }
      }
    }
  }
});
