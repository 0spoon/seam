import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from './authStore';

// Reset store state before each test
beforeEach(() => {
  useAuthStore.setState({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    error: null,
  });
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('authStore', () => {
  it('has correct initial state', () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.error).toBeNull();
  });

  it('login sets user and isAuthenticated on success', async () => {
    const mockResponse = {
      user: { id: '1', username: 'test', email: 'test@example.com' },
      tokens: { access_token: 'at', refresh_token: 'rt' },
    };

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockResponse), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useAuthStore.getState().login('test', 'pass');

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.user?.username).toBe('test');
    expect(state.isLoading).toBe(false);
  });

  it('login sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'invalid credentials' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(useAuthStore.getState().login('test', 'wrong')).rejects.toThrow();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    // The error message comes from the ApiError thrown by the client.
    // Since there's no refresh token, the 401 response body error is used.
    expect(state.error).toBeTruthy();
    expect(state.isLoading).toBe(false);
  });

  it('register sets user and isAuthenticated on success', async () => {
    const mockResponse = {
      user: { id: '2', username: 'newuser', email: 'new@example.com' },
      tokens: { access_token: 'at2', refresh_token: 'rt2' },
    };

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockResponse), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useAuthStore.getState().register('newuser', 'new@example.com', 'pass');

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.user?.username).toBe('newuser');
  });

  it('clearError removes the error', () => {
    useAuthStore.setState({ error: 'some error' });
    useAuthStore.getState().clearError();
    expect(useAuthStore.getState().error).toBeNull();
  });

  it('logout clears user state', async () => {
    useAuthStore.setState({
      user: { id: '1', username: 'test', email: 'test@example.com' },
      isAuthenticated: true,
    });

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    await useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
  });
});
