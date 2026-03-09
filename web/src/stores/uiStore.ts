import { create } from 'zustand';
import type { TagCount } from '../api/types';
import * as api from '../api/client';
import { useSettingsStore } from './settingsStore';

type ViewMode = 'editor' | 'split' | 'preview';

interface UIState {
  sidebarCollapsed: boolean;
  sidebarOpen: boolean;
  rightPanelOpen: boolean;
  editorViewMode: ViewMode;
  commandPaletteOpen: boolean;
  captureModalOpen: boolean;
  captureDefaultProjectId: string;
  tags: TagCount[];
  settingsBridged: boolean;

  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarOpen: (open: boolean) => void;
  toggleRightPanel: () => void;
  setRightPanelOpen: (open: boolean) => void;
  setEditorViewMode: (mode: ViewMode) => void;
  setCommandPaletteOpen: (open: boolean) => void;
  setCaptureModalOpen: (open: boolean, defaultProjectId?: string) => void;
  fetchTags: () => Promise<void>;
  bridgeFromSettings: () => void;
}

export const useUIStore = create<UIState>((set) => ({
  sidebarCollapsed: false,
  sidebarOpen: true,
  rightPanelOpen: true,
  editorViewMode: 'split',
  commandPaletteOpen: false,
  captureModalOpen: false,
  captureDefaultProjectId: '',
  tags: [],
  settingsBridged: false,

  toggleSidebar: () =>
    set((state) => {
      const next = !state.sidebarCollapsed;
      useSettingsStore.getState().updateSetting('sidebar_collapsed', String(next));
      return { sidebarCollapsed: next };
    }),

  setSidebarCollapsed: (collapsed) => {
    useSettingsStore.getState().updateSetting('sidebar_collapsed', String(collapsed));
    set({ sidebarCollapsed: collapsed });
  },
  setSidebarOpen: (open) => set({ sidebarOpen: open }),

  toggleRightPanel: () =>
    set((state) => {
      const next = !state.rightPanelOpen;
      useSettingsStore.getState().updateSetting('right_panel_open', String(next));
      return { rightPanelOpen: next };
    }),

  setRightPanelOpen: (open) => {
    useSettingsStore.getState().updateSetting('right_panel_open', String(open));
    set({ rightPanelOpen: open });
  },

  setEditorViewMode: (mode) => {
    useSettingsStore.getState().updateSetting('editor_view_mode', mode);
    set({ editorViewMode: mode });
  },

  setCommandPaletteOpen: (open) => set({ commandPaletteOpen: open }),
  setCaptureModalOpen: (open, defaultProjectId) =>
    set({ captureModalOpen: open, captureDefaultProjectId: open ? (defaultProjectId ?? '') : '' }),

  fetchTags: async () => {
    try {
      const tags = await api.listTags();
      set({ tags });
    } catch {
      // Ignore tag fetch errors
    }
  },

  // Bridge settings into runtime UI state. Called once after settings load.
  bridgeFromSettings: () => {
    const settings = useSettingsStore.getState();
    set({
      sidebarCollapsed: settings.get('sidebar_collapsed') === 'true',
      rightPanelOpen: settings.get('right_panel_open') !== 'false',
      editorViewMode: (settings.get('editor_view_mode') as ViewMode) || 'split',
      settingsBridged: true,
    });
  },
}));
