import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUIStore } from './uiStore';

vi.mock('../api/client');
vi.mock('./settingsStore', () => {
  const settings: Record<string, string> = {};
  return {
    useSettingsStore: {
      getState: () => ({
        updateSetting: vi.fn(),
        get: (key: string) => settings[key] ?? '',
        settings,
        isLoaded: true,
        fetchSettings: vi.fn(),
      }),
      // Allow tests to inject settings values for bridgeFromSettings
      _setMockSettings: (s: Record<string, string>) => {
        Object.assign(settings, s);
      },
    },
  };
});

import * as api from '../api/client';
import { useSettingsStore } from './settingsStore';

beforeEach(() => {
  useUIStore.setState({
    sidebarCollapsed: false,
    sidebarOpen: true,
    rightPanelOpen: true,
    editorViewMode: 'split',
    commandPaletteOpen: false,
    captureModalOpen: false,
    captureDefaultProjectId: '',
    tags: [],
    settingsBridged: false,
  });
  vi.restoreAllMocks();
});

describe('uiStore', () => {
  it('has correct initial state', () => {
    const state = useUIStore.getState();
    expect(state.sidebarCollapsed).toBe(false);
    expect(state.sidebarOpen).toBe(true);
    expect(state.rightPanelOpen).toBe(true);
    expect(state.editorViewMode).toBe('split');
    expect(state.commandPaletteOpen).toBe(false);
    expect(state.captureModalOpen).toBe(false);
    expect(state.captureDefaultProjectId).toBe('');
    expect(state.tags).toEqual([]);
    expect(state.settingsBridged).toBe(false);
  });

  it('toggleSidebar toggles sidebarCollapsed', () => {
    expect(useUIStore.getState().sidebarCollapsed).toBe(false);

    useUIStore.getState().toggleSidebar();
    expect(useUIStore.getState().sidebarCollapsed).toBe(true);

    useUIStore.getState().toggleSidebar();
    expect(useUIStore.getState().sidebarCollapsed).toBe(false);
  });

  it('setSidebarOpen sets sidebarOpen', () => {
    useUIStore.getState().setSidebarOpen(false);
    expect(useUIStore.getState().sidebarOpen).toBe(false);

    useUIStore.getState().setSidebarOpen(true);
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('setCommandPaletteOpen sets commandPaletteOpen', () => {
    useUIStore.getState().setCommandPaletteOpen(true);
    expect(useUIStore.getState().commandPaletteOpen).toBe(true);

    useUIStore.getState().setCommandPaletteOpen(false);
    expect(useUIStore.getState().commandPaletteOpen).toBe(false);
  });

  it('setCaptureModalOpen sets captureModalOpen and defaultProjectId', () => {
    useUIStore.getState().setCaptureModalOpen(true, 'proj-123');

    const state = useUIStore.getState();
    expect(state.captureModalOpen).toBe(true);
    expect(state.captureDefaultProjectId).toBe('proj-123');
  });

  it('setCaptureModalOpen clears defaultProjectId when closing', () => {
    // First open with a project ID
    useUIStore.getState().setCaptureModalOpen(true, 'proj-123');
    expect(useUIStore.getState().captureDefaultProjectId).toBe('proj-123');

    // Close -- should clear the project ID
    useUIStore.getState().setCaptureModalOpen(false);
    expect(useUIStore.getState().captureModalOpen).toBe(false);
    expect(useUIStore.getState().captureDefaultProjectId).toBe('');
  });

  it('fetchTags populates tags array', async () => {
    const mockTags = [
      { tag: 'go', count: 5 },
      { tag: 'rust', count: 3 },
    ];
    vi.mocked(api.listTags).mockResolvedValueOnce(mockTags);

    await useUIStore.getState().fetchTags();

    expect(useUIStore.getState().tags).toEqual(mockTags);
  });

  it('bridgeFromSettings reads from settingsStore', () => {
    // Inject mock settings values
    const mockSettingsStore = useSettingsStore as unknown as {
      _setMockSettings: (s: Record<string, string>) => void;
    };
    mockSettingsStore._setMockSettings({
      sidebar_collapsed: 'true',
      right_panel_open: 'false',
      editor_view_mode: 'editor',
    });

    useUIStore.getState().bridgeFromSettings();

    const state = useUIStore.getState();
    expect(state.sidebarCollapsed).toBe(true);
    expect(state.rightPanelOpen).toBe(false);
    expect(state.editorViewMode).toBe('editor');
    expect(state.settingsBridged).toBe(true);
  });
});
