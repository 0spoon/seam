import { create } from 'zustand';
import type { Project, CreateProjectReq, UpdateProjectReq } from '../api/types';
import * as api from '../api/client';

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
    }
  },

  createProject: async (req: CreateProjectReq) => {
    const project = await api.createProject(req);
    set((state) => ({ projects: [...state.projects, project] }));
    return project;
  },

  updateProject: async (id: string, req: UpdateProjectReq) => {
    const updated = await api.updateProject(id, req);
    set((state) => ({
      projects: state.projects.map((p) => (p.id === id ? updated : p)),
      currentProject: state.currentProject?.id === id ? updated : state.currentProject,
    }));
    return updated;
  },

  deleteProject: async (id: string, cascade: 'inbox' | 'delete') => {
    await api.deleteProject(id, cascade);
    set((state) => ({
      projects: state.projects.filter((p) => p.id !== id),
      currentProject: state.currentProject?.id === id ? null : state.currentProject,
    }));
  },

  clearError: () => set({ error: null }),
}));
