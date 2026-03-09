import { create } from 'zustand';
import type { Project, CreateProjectReq, UpdateProjectReq } from '../api/types';
import * as api from '../api/client';
import { useToastStore } from '../components/Toast/ToastContainer';
import { useNoteStore } from './noteStore';

interface ProjectState {
  projects: Project[];
  currentProject: Project | null;
  isLoading: boolean;
  error: string | null;

  fetchProjects: () => Promise<void>;
  fetchProject: (id: string) => Promise<void>;
  createProject: (req: CreateProjectReq) => Promise<Project>;
  updateProject: (id: string, req: UpdateProjectReq) => Promise<Project>;
  deleteProject: (id: string, cascade: 'inbox' | 'delete') => Promise<void>;
  clearError: () => void;
}

export const useProjectStore = create<ProjectState>((set) => ({
  projects: [],
  currentProject: null,
  isLoading: false,
  error: null,

  fetchProjects: async () => {
    set({ isLoading: true, error: null });
    try {
      const projects = await api.listProjects();
      set({ projects, isLoading: false });
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch projects';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
    }
  },

  fetchProject: async (id: string) => {
    set({ isLoading: true, error: null });
    try {
      const project = await api.getProject(id);
      set({ currentProject: project, isLoading: false });
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch project';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
    }
  },

  createProject: async (req: CreateProjectReq) => {
    try {
      const project = await api.createProject(req);
      set((state) => ({ projects: [...state.projects, project] }));
      useToastStore.getState().addToast('Project created', 'success');
      return project;
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to create project';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  updateProject: async (id: string, req: UpdateProjectReq) => {
    try {
      const updated = await api.updateProject(id, req);
      set((state) => ({
        projects: state.projects.map((p) => (p.id === id ? updated : p)),
        currentProject: state.currentProject?.id === id ? updated : state.currentProject,
      }));
      return updated;
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to update project';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  deleteProject: async (id: string, cascade: 'inbox' | 'delete') => {
    try {
      await api.deleteProject(id, cascade);
      set((state) => ({
        projects: state.projects.filter((p) => p.id !== id),
        currentProject: state.currentProject?.id === id ? null : state.currentProject,
      }));
      useToastStore.getState().addToast('Project deleted', 'success');
      // Refresh note list since cascade may have moved or deleted notes.
      useNoteStore.getState().fetchNotes();
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to delete project';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  clearError: () => set({ error: null }),
}));
