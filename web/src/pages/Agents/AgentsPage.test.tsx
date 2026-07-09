import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AgentSession } from '../../api/types';

// Capture the WebSocket subscriber so tests can dispatch synthetic events.
const hoisted = vi.hoisted(() => ({
  handler: null as null | ((msg: { type: string; payload: unknown }) => void),
}));

vi.mock('../../api/ws', () => ({
  subscribe: (h: (msg: { type: string; payload: unknown }) => void) => {
    hoisted.handler = h;
    return () => {};
  },
}));

vi.mock('../../api/client');

import * as api from '../../api/client';
import { AgentsPage } from './AgentsPage';
import { useAgentStore } from '../../stores/agentStore';
import { useProjectStore } from '../../stores/projectStore';

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

function renderPage() {
  return render(
    <MemoryRouter>
      <AgentsPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.handler = null;
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
  useProjectStore.setState({ projects: [], currentProject: null, isLoading: false, error: null });
  vi.mocked(api.getAgentMemories).mockResolvedValue([]);
});

describe('AgentsPage', () => {
  it('renders the sessions returned by the API', async () => {
    vi.mocked(api.getAgentSessions).mockResolvedValue([
      makeSession({ ID: 's1', Name: 'refactor-auth' }),
      makeSession({ ID: 's2', Name: 'lab/analyze-crash', Status: 'completed' }),
    ]);

    renderPage();

    expect(await screen.findByText('refactor-auth')).toBeInTheDocument();
    expect(await screen.findByText('lab/analyze-crash')).toBeInTheDocument();
    // lab/* sessions get a "lab" badge.
    expect(screen.getByText('lab')).toBeInTheDocument();
  });

  it('refetches sessions on a synthetic agent.session_ended WS event', async () => {
    vi.mocked(api.getAgentSessions)
      .mockResolvedValueOnce([makeSession({ ID: 's1', Name: 'session-one' })])
      .mockResolvedValueOnce([
        makeSession({ ID: 's1', Name: 'session-one', Status: 'completed' }),
        makeSession({ ID: 's2', Name: 'session-two' }),
      ]);

    renderPage();

    expect(await screen.findByText('session-one')).toBeInTheDocument();
    expect(screen.queryByText('session-two')).not.toBeInTheDocument();

    // Dispatch the WS event the backend emits when a session ends.
    await act(async () => {
      hoisted.handler?.({ type: 'agent.session_ended', payload: { session_name: 'session-one' } });
    });

    expect(await screen.findByText('session-two')).toBeInTheDocument();
    expect(api.getAgentSessions).toHaveBeenCalledTimes(2);
  });
});
