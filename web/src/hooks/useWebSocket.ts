import { useEffect, useCallback } from 'react';
import { subscribe } from '../api/ws';
import type { WSMessage } from '../api/types';
import { useNoteStore } from '../stores/noteStore';
import { useAgentStore } from '../stores/agentStore';
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

/**
 * Subscribes to agent session/memory WebSocket events and refreshes the agent
 * store so the Agents page stays live. Mount once on the Agents page.
 */
export function useAgentWebSocket() {
  const handleSessionEvent = useAgentStore((s) => s.handleSessionEvent);
  const handleMemoryEvent = useAgentStore((s) => s.handleMemoryEvent);

  const handler = useCallback(
    (msg: WSMessage) => {
      if (msg.type === 'agent.session_started' || msg.type === 'agent.session_ended') {
        handleSessionEvent();
      } else if (msg.type === 'agent.memory_changed') {
        handleMemoryEvent();
      }
    },
    [handleSessionEvent, handleMemoryEvent],
  );

  useWebSocket(handler);
}
