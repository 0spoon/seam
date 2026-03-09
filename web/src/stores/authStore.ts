import { create } from 'zustand';
import type { UserInfo } from '../api/types';
import * as api from '../api/client';
import { connect, disconnect } from '../api/ws';
import { useToastStore } from '../components/Toast/ToastContainer';

// localStorage key used to persist the user email across page refreshes.
// The JWT only contains sub (id) and username; email is not in the token.
const EMAIL_STORAGE_KEY = 'seam_user_email';

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
      localStorage.setItem(EMAIL_STORAGE_KEY, res.user.email);
      set({ user: res.user, isAuthenticated: true, isLoading: false });
      connect();
      useToastStore.getState().addToast('Logged in successfully', 'success');
    } catch (err) {
      const message =
        err instanceof api.ApiError ? err.message : 'Login failed';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  register: async (username, email, password) => {
    set({ isLoading: true, error: null });
    try {
      const res = await api.register({ username, email, password });
      localStorage.setItem(EMAIL_STORAGE_KEY, res.user.email);
      set({ user: res.user, isAuthenticated: true, isLoading: false });
      connect();
      useToastStore.getState().addToast('Account created successfully', 'success');
    } catch (err) {
      const message =
        err instanceof api.ApiError ? err.message : 'Registration failed';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  logout: async () => {
    disconnect();
    await api.logout();
    localStorage.removeItem(EMAIL_STORAGE_KEY);
    set({ user: null, isAuthenticated: false, error: null });
    useToastStore.getState().addToast('Logged out', 'info');
  },

  restoreSession: async () => {
    const refreshToken = api.getRefreshToken();
    if (!refreshToken) {
      set({ isLoading: false });
      return;
    }

    try {
      // Use the client's tryRefresh() to avoid duplicating refresh logic.
      const ok = await api.tryRefresh();
      if (!ok) {
        api.setTokens(null);
        set({ isLoading: false });
        return;
      }

      const token = api.getAccessToken();
      if (!token) {
        set({ isLoading: false });
        return;
      }

      const payload = parseJWT(token);
      if (payload) {
        // Restore email from localStorage. The JWT does not carry the
        // email claim, so after a page refresh we rely on the cached value.
        const storedEmail = localStorage.getItem(EMAIL_STORAGE_KEY) ?? '';
        set({
          user: {
            id: payload.sub as string,
            username: (payload.username as string) ?? '',
            email: storedEmail,
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
