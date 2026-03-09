import { create } from 'zustand';
import type { UserInfo } from '../api/types';
import * as api from '../api/client';
import { connect, disconnect } from '../api/ws';

interface AuthState {
  user: UserInfo | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;

  login: (username: string, password: string) => Promise<void>;
  register: (
    username: string,
    email: string,
    password: string,
  ) => Promise<void>;
  logout: () => Promise<void>;
  restoreSession: () => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isAuthenticated: false,
  isLoading: true,
  error: null,

  login: async (username, password) => {
    set({ isLoading: true, error: null });
    try {
      const res = await api.login({ username, password });
      set({ user: res.user, isAuthenticated: true, isLoading: false });
      connect();
    } catch (err) {
      const message =
        err instanceof api.ApiError ? err.message : 'Login failed';
      set({ error: message, isLoading: false });
      throw err;
    }
  },

  register: async (username, email, password) => {
    set({ isLoading: true, error: null });
    try {
      const res = await api.register({ username, email, password });
      set({ user: res.user, isAuthenticated: true, isLoading: false });
      connect();
    } catch (err) {
      const message =
        err instanceof api.ApiError ? err.message : 'Registration failed';
      set({ error: message, isLoading: false });
      throw err;
    }
  },

  logout: async () => {
    disconnect();
    await api.logout();
    set({ user: null, isAuthenticated: false, error: null });
  },

  restoreSession: async () => {
    const refreshToken = api.getRefreshToken();
    if (!refreshToken) {
      set({ isLoading: false });
      return;
    }

    try {
      // Try to refresh the access token
      const res = await fetch('/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });

      if (!res.ok) {
        api.setTokens(null);
        set({ isLoading: false });
        return;
      }

      const tokens = await res.json();
      api.setTokens({ access_token: tokens.access_token, refresh_token: refreshToken });

      // Fetch user info using the health endpoint or decode JWT
      // For now, parse the JWT to extract user info
      const payload = parseJWT(tokens.access_token);
      if (payload) {
        set({
          user: {
            id: payload.sub as string,
            username: (payload.username as string) ?? '',
            email: '',
          },
          isAuthenticated: true,
          isLoading: false,
        });
        connect();
      } else {
        set({ isLoading: false });
      }
    } catch {
      api.setTokens(null);
      set({ isLoading: false });
    }
  },

  clearError: () => set({ error: null }),
}));

function parseJWT(token: string): Record<string, unknown> | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    const payload = atob(parts[1].replace(/-/g, '+').replace(/_/g, '/'));
    return JSON.parse(payload);
  } catch {
    return null;
  }
}
