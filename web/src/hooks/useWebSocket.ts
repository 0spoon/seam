import { useEffect, useCallback } from 'react';
import { subscribe } from '../api/ws';
import type { WSMessage } from '../api/types';
import { useNoteStore } from '../stores/noteStore';
import { invalidateCache } from '../lib/wikilinkCache';

export function useWebSocket(handler: (msg: WSMessage) => void) {
  useEffect(() => {
    const unsubscribe = subscribe(handler);
    return unsubscribe;
  }, [handler]);
}

/**
 * Subscribes to WebSocket events that affect the note store and
 * dispatches updates automatically. Mount once in the app shell.
 */
export function useNoteWebSocket() {
  const handleNoteChanged = useNoteStore((s) => s.handleNoteChanged);

  const handler = useCallback(
    (msg: WSMessage) => {
      if (msg.type === 'note.changed') {
        invalidateCache();
        const payload = msg.payload as { note_id?: string };
        if (payload.note_id) {
          handleNoteChanged(payload.note_id);
        }
      }
    },
    [handleNoteChanged],
  );

  useWebSocket(handler);
}
