import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAgentStore } from './agentStore';
import type { AgentSession, AgentMemory } from '../api/types';

vi.mock('../api/client');
import * as api from '../api/client';

const makeSession = (overrides: Partial<AgentSession> = {}): AgentSession => ({
  ID: 's1',
  Name: 'refactor-auth',
  ParentSessionID: '',
  Status: 'active',
  Findings: '',
  ProjectSlug: 'seam',
  Metadata: {},
  CreatedAt: '2026-07-01T00:00:00Z',
  UpdatedAt: '2026-07-01T00:00:00Z',
  ...overrides,
});

const makeMemory = (overrides: Partial<AgentMemory> = {}): AgentMemory => ({
  category: 'gotcha',
  name: 'wal-mode',
  title: 'WAL mode gotcha',
  note_id: 'n1',
  description: 'Enable WAL',
  project: 'seam',
  updated_at: '2026-07-01T00:00:00Z',
  ...overrides,
});

beforeEach(() => {
  useAgentStore.setState({
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
  });
  vi.clearAllMocks();
});

describe('agentStore', () => {
  it('fetchSessions populates sessions and stores the filter', async () => {
    const sessions = [makeSession(), makeSession({ ID: 's2', Name: 'lab/thing' })];
    vi.mocked(api.getAgentSessions).mockResolvedValueOnce(sessions);

    await useAgentStore.getState().fetchSessions('active', 'seam');

    const state = useAgentStore.getState();
    expect(state.sessions).toHaveLength(2);
    expect(state.sessionsLoading).toBe(false);
    expect(state.sessionStatus).toBe('active');
    expect(state.sessionProject).toBe('seam');
    expect(api.getAgentSessions).toHaveBeenCalledWith('active', 'seam');
  });

  it('fetchSessions sets an error message on failure', async () => {
    vi.mocked(api.getAgentSessions).mockRejectedValueOnce(new Error('boom'));

    await useAgentStore.getState().fetchSessions();

    const state = useAgentStore.getState();
    expect(state.sessionsError).toBe('Failed to load sessions');
    expect(state.sessionsLoading).toBe(false);
  });

  it('fetchMemories populates memories', async () => {
    vi.mocked(api.getAgentMemories).mockResolvedValueOnce([makeMemory()]);

    await useAgentStore.getState().fetchMemories('seam', 'gotcha');

    const state = useAgentStore.getState();
    expect(state.memories).toHaveLength(1);
    expect(state.memoryProject).toBe('seam');
    expect(state.memoryCategory).toBe('gotcha');
  });

  it('fetchMemories sets an error message on failure', async () => {
    vi.mocked(api.getAgentMemories).mockRejectedValueOnce(new Error('boom'));

    await useAgentStore.getState().fetchMemories();

    expect(useAgentStore.getState().memoriesError).toBe('Failed to load memories');
  });

  it('handleSessionEvent refetches with the persisted filter', async () => {
    useAgentStore.setState({ sessionStatus: 'active', sessionProject: 'seam' });
    vi.mocked(api.getAgentSessions).mockResolvedValueOnce([makeSession()]);

    useAgentStore.getState().handleSessionEvent();
    // Allow the fire-and-forget refetch to settle.
    await vi.waitFor(() => expect(api.getAgentSessions).toHaveBeenCalled());

    expect(api.getAgentSessions).toHaveBeenCalledWith('active', 'seam');
  });
});
