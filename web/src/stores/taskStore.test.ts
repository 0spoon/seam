import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useTaskStore } from './taskStore';
import type { Task } from '../api/types';

vi.mock('../api/client');
const addToastMock = vi.fn();
vi.mock('../components/Toast/ToastContainer', () => ({
  useToastStore: {
    getState: () => ({ addToast: addToastMock, toasts: [], removeToast: vi.fn() }),
  },
}));

import * as api from '../api/client';

const makeTask = (overrides: Partial<Task> = {}): Task => ({
  id: 't1',
  note_id: 'n1',
  line_number: 3,
  content: 'Do the thing',
  done: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  ...overrides,
});

beforeEach(() => {
  useTaskStore.setState({
    tasks: [],
    summary: { total: 0, done: 0, open: 0 },
    total: 0,
    isLoading: false,
    error: null,
  });
  vi.clearAllMocks();
});

describe('taskStore', () => {
  it('fetchTasks populates tasks and total', async () => {
    vi.mocked(api.getTasks).mockResolvedValueOnce({ tasks: [makeTask()], total: 1 });

    await useTaskStore.getState().fetchTasks({ done: false });

    const state = useTaskStore.getState();
    expect(state.tasks).toHaveLength(1);
    expect(state.total).toBe(1);
    expect(state.isLoading).toBe(false);
  });

  it('fetchTasks sets an error message on failure', async () => {
    vi.mocked(api.getTasks).mockRejectedValueOnce(new Error('boom'));

    await useTaskStore.getState().fetchTasks();

    expect(useTaskStore.getState().error).toBe('Failed to load tasks');
    expect(useTaskStore.getState().isLoading).toBe(false);
  });

  it('fetchSummary populates the summary', async () => {
    vi.mocked(api.getTaskSummary).mockResolvedValueOnce({ total: 5, done: 2, open: 3 });

    await useTaskStore.getState().fetchSummary();

    expect(useTaskStore.getState().summary).toEqual({ total: 5, done: 2, open: 3 });
  });

  it('toggleTask optimistically flips done', async () => {
    useTaskStore.setState({ tasks: [makeTask({ done: false })] });
    vi.mocked(api.toggleTask).mockResolvedValueOnce(undefined);

    const promise = useTaskStore.getState().toggleTask('t1', true);

    // Optimistic update is synchronous.
    expect(useTaskStore.getState().tasks[0].done).toBe(true);

    await promise;
    expect(useTaskStore.getState().tasks[0].done).toBe(true);
    expect(api.toggleTask).toHaveBeenCalledWith('t1', true);
  });

  it('toggleTask reverts and toasts on failure', async () => {
    useTaskStore.setState({ tasks: [makeTask({ done: false })] });
    vi.mocked(api.toggleTask).mockRejectedValueOnce(new Error('save failed'));

    await useTaskStore.getState().toggleTask('t1', true);

    // Reverted back to the original value.
    expect(useTaskStore.getState().tasks[0].done).toBe(false);
    expect(addToastMock).toHaveBeenCalledWith('Failed to update task', 'error');
  });
});
