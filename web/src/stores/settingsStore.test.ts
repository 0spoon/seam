import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useSettingsStore } from './settingsStore';

vi.mock('../api/client');
const addToastMock = vi.fn();
vi.mock('../components/Toast/ToastContainer', () => ({
  useToastStore: {
    getState: () => ({ addToast: addToastMock, toasts: [], removeToast: vi.fn() }),
  },
}));

import * as api from '../api/client';

const DEFAULTS: Record<string, string> = {
  editor_view_mode: 'split',
  right_panel_open: 'true',
  sidebar_collapsed: 'false',
  sidebar_projects_expanded: 'true',
  sidebar_tags_expanded: 'true',
};

beforeEach(() => {
  useSettingsStore.setState({ settings: { ...DEFAULTS }, isLoaded: false });
  vi.restoreAllMocks();
});

describe('settingsStore', () => {
  it('has correct initial state with defaults', () => {
    const state = useSettingsStore.getState();
    expect(state.settings).toEqual(DEFAULTS);
    expect(state.isLoaded).toBe(false);
  });

  it('get returns default value for known key', () => {
    expect(useSettingsStore.getState().get('editor_view_mode')).toBe('split');
    expect(useSettingsStore.getState().get('sidebar_collapsed')).toBe('false');
  });

  it('get returns empty string for unknown key', () => {
    expect(useSettingsStore.getState().get('nonexistent_key')).toBe('');
  });

  it('fetchSettings merges remote settings with defaults', async () => {
    const remote = { editor_view_mode: 'preview', custom_key: 'custom_value' };
    vi.mocked(api.getSettings).mockResolvedValueOnce(remote);

    await useSettingsStore.getState().fetchSettings();

    const state = useSettingsStore.getState();
    expect(state.isLoaded).toBe(true);
    expect(state.settings.editor_view_mode).toBe('preview');
    expect(state.settings.sidebar_collapsed).toBe('false');
    expect(state.settings.custom_key).toBe('custom_value');
  });

  it('fetchSettings uses defaults on failure', async () => {
    vi.mocked(api.getSettings).mockRejectedValueOnce(new Error('network error'));

    await useSettingsStore.getState().fetchSettings();

    const state = useSettingsStore.getState();
    expect(state.isLoaded).toBe(true);
    expect(state.settings).toEqual(DEFAULTS);
  });

  it('updateSetting optimistically updates', async () => {
    vi.mocked(api.updateSettings).mockResolvedValueOnce(undefined);

    const promise = useSettingsStore.getState().updateSetting('editor_view_mode', 'editor');

    // Optimistic update is synchronous
    expect(useSettingsStore.getState().settings.editor_view_mode).toBe('editor');

    await promise;

    expect(useSettingsStore.getState().settings.editor_view_mode).toBe('editor');
  });

  it('updateSetting reverts on failure', async () => {
    vi.mocked(api.updateSettings).mockRejectedValueOnce(new Error('save failed'));

    await useSettingsStore.getState().updateSetting('editor_view_mode', 'editor');

    // Should revert to original value
    expect(useSettingsStore.getState().settings.editor_view_mode).toBe('split');
    expect(addToastMock).toHaveBeenCalledWith('Failed to save setting', 'error');
  });
});
