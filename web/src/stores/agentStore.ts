import { create } from 'zustand';
import { getAgentSessions, getAgentMemories } from '../api/client';
import type { AgentSession, AgentMemory } from '../api/types';

export type SessionStatus = 'all' | 'active' | 'completed';

interface AgentStore {
  sessions: AgentSession[];
  memories: AgentMemory[];
  // Persisted filters so WS-triggered refetches reuse the active view.
  sessionStatus: SessionStatus;
  sessionProject: string;
  memoryProject: string;
  memoryCategory: string;
  sessionsLoading: boolean;
  memoriesLoading: boolean;
  sessionsError: string | null;
  memoriesError: string | null;

  fetchSessions: (status?: SessionStatus, project?: string) => Promise<void>;
  fetchMemories: (project?: string, category?: string) => Promise<void>;
  // WS-driven refresh: re-run the current query when the backend signals a
  // session or memory change.
  handleSessionEvent: () => void;
  handleMemoryEvent: () => void;
}

export const useAgentStore = create<AgentStore>((set, get) => ({
  sessions: [],
  memories: [],
  sessionStatus: 'all',
  sessionProject: '',
  memoryProject: '',
  memoryCategory: '',
  sessionsLoading: false,
  memoriesLoading: false,
  sessionsError: null,
  memoriesError: null,

  fetchSessions: async (status, project) => {
    const s = get();
    const st = status ?? s.sessionStatus;
    const pr = project ?? s.sessionProject;
    set({ sessionStatus: st, sessionProject: pr, sessionsLoading: true, sessionsError: null });
    try {
      const sessions = await getAgentSessions(st, pr || undefined);
      set({ sessions, sessionsLoading: false });
    } catch {
      set({ sessionsError: 'Failed to load sessions', sessionsLoading: false });
    }
  },

  fetchMemories: async (project, category) => {
    const s = get();
    const pr = project ?? s.memoryProject;
    const cat = category ?? s.memoryCategory;
    set({ memoryProject: pr, memoryCategory: cat, memoriesLoading: true, memoriesError: null });
    try {
      const memories = await getAgentMemories(pr || undefined, cat || undefined);
      set({ memories, memoriesLoading: false });
    } catch {
      set({ memoriesError: 'Failed to load memories', memoriesLoading: false });
    }
  },

  handleSessionEvent: () => {
    void get().fetchSessions();
  },

  handleMemoryEvent: () => {
    void get().fetchMemories();
  },
}));
