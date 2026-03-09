import { useEffect } from 'react';
import { subscribe } from '../api/ws';
import type { WSMessage } from '../api/types';

export function useWebSocket(handler: (msg: WSMessage) => void) {
  useEffect(() => {
    const unsubscribe = subscribe(handler);
    return unsubscribe;
  }, [handler]);
}
