import { useState, useEffect } from 'react';
import {
  subscribeToState,
  getConnectionState,
  getLastConnectedAt,
  getReconnectAttempts,
} from '../api/ws';

interface ConnectionStatus {
  status: 'connected' | 'reconnecting' | 'disconnected';
  lastConnectedAt: Date | null;
  reconnectAttempts: number;
}

export function useConnectionStatus(): ConnectionStatus {
  const [status, setStatus] = useState<ConnectionStatus>({
    status: getConnectionState(),
    lastConnectedAt: getLastConnectedAt(),
    reconnectAttempts: getReconnectAttempts(),
  });

  useEffect(() => {
    return subscribeToState((newState) => {
      setStatus({
        status: newState,
        lastConnectedAt: getLastConnectedAt(),
        reconnectAttempts: getReconnectAttempts(),
      });
    });
  }, []);

  return status;
}
