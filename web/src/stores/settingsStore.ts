import { create } from 'zustand';
import * as api from '../api/client';
import { useToastStore } from '../components/Toast/ToastContainer';

// Default values used when a user has no saved settings.
const DEFAULTS: Record<string, string> = {
  editor_view_mode: 'split',
  right_panel_open: 'true',
  sidebar_collapsed: 'false',
  sidebar_projects_expanded: 'true',
  sidebar_tags_expanded: 'true',
};

interface SettingsState {
  settings: Record<string, string>;
  isLoaded: boolean;

  fetchSettings: () => Promise<void>;
  updateSetting: (key: string, value: string) => Promise<void>;
  get: (key: string) => string;
}

export const useSettingsStore = create<SettingsState>((set, get) => ({
  settings: { ...DEFAULTS },
  isLoaded: false,

  fetchSettings: async () => {
    try {
      const remote = await api.getSettings();
      set({ settings: { ...DEFAULTS, ...remote }, isLoaded: true });
    } catch {
      // Use defaults on failure.
      set({ isLoaded: true });
    }
  },

  updateSetting: async (key: string, value: string) => {
    const prev = get().settings[key];
    // Optimistic update.
    set((state) => ({
      settings: { ...state.settings, [key]: value },
    }));

    try {
      await api.updateSettings({ [key]: value });
    } catch {
      // Revert on failure.
      set((state) => ({
        settings: { ...state.settings, [key]: prev ?? DEFAULTS[key] ?? '' },
      }));
      useToastStore.getState().addToast('Failed to save setting', 'error');
    }
  },

  get: (key: string) => {
    return get().settings[key] ?? DEFAULTS[key] ?? '';
  },
}));
