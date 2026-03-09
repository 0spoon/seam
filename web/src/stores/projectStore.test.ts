import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useProjectStore } from './projectStore';
import type { Project } from '../api/types';

const makeProject = (overrides: Partial<Project> = {}): Project => ({
  id: 'proj1',
  name: 'Test Project',
  slug: 'test-project',
  description: 'A test project',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  ...overrides,
});

beforeEach(() => {
  useProjectStore.setState({
    projects: [],
    currentProject: null,
    isLoading: false,
    error: null,
  });
  vi.restoreAllMocks();
});

describe('projectStore', () => {
  it('has correct initial state', () => {
    const state = useProjectStore.getState();
    expect(state.projects).toEqual([]);
    expect(state.currentProject).toBeNull();
    expect(state.isLoading).toBe(false);
  });

  it('fetchProjects populates the projects array', async () => {
    const mockProjects = [makeProject(), makeProject({ id: 'proj2', name: 'Second' })];

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockProjects), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useProjectStore.getState().fetchProjects();

    const state = useProjectStore.getState();
    expect(state.projects).toHaveLength(2);
    expect(state.isLoading).toBe(false);
  });

  it('createProject adds project to array', async () => {
    const mockProject = makeProject();

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockProject), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    const result = await useProjectStore.getState().createProject({
      name: 'Test Project',
    });

    expect(result.id).toBe('proj1');
    expect(useProjectStore.getState().projects).toHaveLength(1);
  });

  it('deleteProject removes project from array', async () => {
    useProjectStore.setState({
      projects: [makeProject(), makeProject({ id: 'proj2' })],
    });

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    await useProjectStore.getState().deleteProject('proj1', 'inbox');

    expect(useProjectStore.getState().projects).toHaveLength(1);
    expect(useProjectStore.getState().projects[0].id).toBe('proj2');
  });

  it('updateProject updates project in array', async () => {
    const original = makeProject();
    useProjectStore.setState({
      projects: [original],
      currentProject: original,
    });

    const updated = { ...original, name: 'Renamed' };

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(updated), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useProjectStore.getState().updateProject('proj1', { name: 'Renamed' });

    expect(useProjectStore.getState().projects[0].name).toBe('Renamed');
    expect(useProjectStore.getState().currentProject?.name).toBe('Renamed');
  });
});
