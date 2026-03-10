import type { WSMessage } from './types';
import { getAccessToken, getRefreshToken, tryRefresh, setTokens } from './client';

type MessageHandler = (msg: WSMessage) => void;
type ConnectionState = 'connected' | 'disconnected' | 'reconnecting';
type StateChangeHandler = (state: ConnectionState) => void;

let socket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
let reconnectAttempts = 0;
let connectionState: ConnectionState = 'disconnected';
let lastConnectedAt: Date | null = null;
const MAX_RECONNECT_DELAY = 30000;
const HEARTBEAT_INTERVAL_MS = 30000;
const handlers: Set<MessageHandler> = new Set();
const stateHandlers = new Set<StateChangeHandler>();

function setConnectionState(newState: ConnectionState) {
  if (connectionState !== newState) {
    connectionState = newState;
    if (newState === 'connected') {
      lastConnectedAt = new Date();
      reconnectAttempts = 0;
    }
    stateHandlers.forEach((h) => h(newState));
  }
}

export function subscribeToState(handler: StateChangeHandler): () => void {
  stateHandlers.add(handler);
  handler(connectionState);
  return () => stateHandlers.delete(handler);
}

export function getConnectionState(): ConnectionState {
  return connectionState;
}

export function getLastConnectedAt(): Date | null {
  return lastConnectedAt;
}

export function getReconnectAttempts(): number {
  return reconnectAttempts;
}

function getWsUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/api/ws`;
}

export function connect() {
  if (socket?.readyState === WebSocket.OPEN) return;

  const token = getAccessToken();
  if (!token) return;

  try {
    socket = new WebSocket(getWsUrl());

    socket.onopen = () => {
      // Fetch a fresh token at open time in case a refresh occurred
      // between connect() and the WebSocket handshake completing.
      const freshToken = getAccessToken() || token;
      socket?.send(JSON.stringify({ type: 'auth', payload: { token: freshToken } }));

      setConnectionState('connected');

      // Start periodic heartbeat to detect dead connections behind NAT
      // or load balancers with idle timeouts.
      stopHeartbeat();
      heartbeatTimer = setInterval(() => {
        if (socket?.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: 'ping' }));
        }
      }, HEARTBEAT_INTERVAL_MS);
    };

    socket.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        handlers.forEach((handler) => handler(msg));
      } catch {
        // Ignore malformed messages
      }
    };

    socket.onclose = () => {
      socket = null;
      stopHeartbeat();
      setConnectionState('disconnected');
      scheduleReconnect();
    };

    socket.onerror = () => {
      socket?.close();
    };
  } catch {
    scheduleReconnect();
  }
}

function stopHeartbeat() {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

async function scheduleReconnect() {
  if (reconnectTimer) return;

  // Always refresh the access token before reconnecting. The in-memory
  // token string may still be present but expired, and unlike the HTTP
  // client the WebSocket auth handshake has no 401 retry path.
  const refresh = getRefreshToken();
  if (!refresh) return;

  const ok = await tryRefresh();
  if (!ok) {
    setTokens(null);
    return;
  }

  reconnectAttempts++;
  setConnectionState('reconnecting');
  const delay = Math.min(1000 * 2 ** (reconnectAttempts - 1), MAX_RECONNECT_DELAY);
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, delay);
}

export function disconnect() {
  stopHeartbeat();
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  reconnectAttempts = 0;
  if (socket) {
    socket.onclose = null;
    socket.close();
    socket = null;
  }
  setConnectionState('disconnected');
}

export function subscribe(handler: MessageHandler): () => void {
  handlers.add(handler);
  return () => handlers.delete(handler);
}

export function send(type: string, payload: unknown) {
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify({ type, payload }));
  }
}

export function isConnected(): boolean {
  return socket?.readyState === WebSocket.OPEN;
}
