import { create } from 'zustand';
import type { TagCount } from '../api/types';
import * as api from '../api/client';

interface UIState {
  sidebarCollapsed: boolean;
  sidebarOpen: boolean;
  rightPanelOpen: boolean;
  commandPaletteOpen: boolean;
  captureModalOpen: boolean;
  captureDefaultProjectId: string;
  tags: TagCount[];

  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarOpen: (open: boolean) => void;
  toggleRightPanel: () => void;
  setRightPanelOpen: (open: boolean) => void;
  setCommandPaletteOpen: (open: boolean) => void;
  setCaptureModalOpen: (open: boolean, defaultProjectId?: string) => void;
  fetchTags: () => Promise<void>;
}

export const useUIStore = create<UIState>((set) => ({
  sidebarCollapsed: false,
  sidebarOpen: true,
  rightPanelOpen: false,
  commandPaletteOpen: false,
  captureModalOpen: false,
  captureDefaultProjectId: '',
  tags: [],

  toggleSidebar: () =>
    set((state) => ({
      sidebarCollapsed: !state.sidebarCollapsed,
    })),

  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),

  toggleRightPanel: () =>
    set((state) => ({ rightPanelOpen: !state.rightPanelOpen })),

  setRightPanelOpen: (open) => set({ rightPanelOpen: open }),
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
}));
