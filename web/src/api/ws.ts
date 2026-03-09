import type { WSMessage } from './types';
import { getAccessToken } from './client';

type MessageHandler = (msg: WSMessage) => void;

let socket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectAttempts = 0;
const MAX_RECONNECT_DELAY = 30000;
const handlers: Set<MessageHandler> = new Set();

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
      reconnectAttempts = 0;
      // Send auth message as first message
      socket?.send(JSON.stringify({ type: 'auth', payload: { token } }));
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
      scheduleReconnect();
    };

    socket.onerror = () => {
      socket?.close();
    };
  } catch {
    scheduleReconnect();
  }
}

function scheduleReconnect() {
  if (reconnectTimer) return;
  const delay = Math.min(1000 * 2 ** reconnectAttempts, MAX_RECONNECT_DELAY);
  reconnectAttempts++;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, delay);
}

export function disconnect() {
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
