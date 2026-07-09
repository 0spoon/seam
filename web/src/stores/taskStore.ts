import { create } from 'zustand';
import * as api from '../api/client';
import type { Task, TaskSummary, TaskFilter } from '../api/types';
import { useToastStore } from '../components/Toast/ToastContainer';

interface TaskStore {
  tasks: Task[];
  summary: TaskSummary;
  total: number;
  isLoading: boolean;
  error: string | null;

  fetchTasks: (filter?: TaskFilter) => Promise<void>;
  fetchSummary: (projectId?: string) => Promise<void>;
  toggleTask: (id: string, done: boolean) => Promise<void>;
}

const EMPTY_SUMMARY: TaskSummary = { total: 0, done: 0, open: 0 };

export const useTaskStore = create<TaskStore>((set, get) => ({
  tasks: [],
  summary: EMPTY_SUMMARY,
  total: 0,
  isLoading: false,
  error: null,

  fetchTasks: async (filter = {}) => {
    set({ isLoading: true, error: null });
    try {
      const { tasks, total } = await api.getTasks(filter);
      set({ tasks, total, isLoading: false });
    } catch {
      set({ error: 'Failed to load tasks', isLoading: false });
    }
  },

  fetchSummary: async (projectId) => {
    try {
      const summary = await api.getTaskSummary(projectId);
      set({ summary });
    } catch {
      // Summary is supplementary; leave the previous value on failure.
    }
  },

  toggleTask: async (id, done) => {
    const prev = get().tasks;
    // Optimistic: flip the checkbox immediately.
    set({ tasks: prev.map((t) => (t.id === id ? { ...t, done } : t)) });
    try {
      await api.toggleTask(id, done);
    } catch {
      // Revert on failure.
      set({ tasks: prev });
      useToastStore.getState().addToast('Failed to update task', 'error');
    }
  },
}));
