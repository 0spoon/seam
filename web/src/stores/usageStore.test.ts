import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useUsageStore } from './usageStore';
import type { UsageSummary, UsageBudget, RetrievalSummary } from '../api/types';

vi.mock('../api/client');
const addToastMock = vi.fn();
vi.mock('../components/Toast/ToastContainer', () => ({
  useToastStore: {
    getState: () => ({ addToast: addToastMock, toasts: [], removeToast: vi.fn() }),
  },
}));

import * as api from '../api/client';

const summary: UsageSummary = {
  total_tokens: 100,
  input_tokens: 60,
  output_tokens: 40,
  billed_tokens: 30,
  local_tokens: 70,
  call_count: 5,
};

const budget: UsageBudget = {
  enabled: true,
  period: 'monthly',
  max_tokens: 1000,
  used_tokens: 100,
  remaining_tokens: 900,
  gate_local: false,
};

const retrieval: RetrievalSummary = {
  since: '2026-06-01T00:00:00Z',
  total: 12,
  kinds: [{ kind: 'briefing', total: 8, hits: 4 }],
  read_after_inject_rate: 0.5,
  injection_events: 8,
  read_followups: 4,
};

function mockAllOk() {
  vi.mocked(api.getUsageSummary).mockResolvedValue(summary);
  vi.mocked(api.getUsageByProvider).mockResolvedValue([]);
  vi.mocked(api.getUsageByModel).mockResolvedValue([]);
  vi.mocked(api.getUsageByFunction).mockResolvedValue([]);
  vi.mocked(api.getUsageTimeseries).mockResolvedValue([]);
  vi.mocked(api.getUsageBudget).mockResolvedValue(budget);
}

beforeEach(() => {
  useUsageStore.setState({
    summary: null,
    byProvider: [],
    byModel: [],
    byFunction: [],
    timeseries: [],
    budget: null,
    retrieval: null,
    isLoading: false,
    error: null,
  });
  vi.clearAllMocks();
});

describe('usageStore', () => {
  it('fetchAll populates all sections', async () => {
    mockAllOk();
    vi.mocked(api.getRetrievalSummary).mockResolvedValueOnce(retrieval);

    await useUsageStore.getState().fetchAll();

    const state = useUsageStore.getState();
    expect(state.summary).toEqual(summary);
    expect(state.budget).toEqual(budget);
    expect(state.retrieval).toEqual(retrieval);
    expect(state.isLoading).toBe(false);
    expect(state.error).toBeNull();
  });

  it('fetchAll tolerates a missing retrieval endpoint', async () => {
    mockAllOk();
    vi.mocked(api.getRetrievalSummary).mockRejectedValueOnce(new Error('404'));

    await useUsageStore.getState().fetchAll();

    const state = useUsageStore.getState();
    expect(state.summary).toEqual(summary);
    expect(state.retrieval).toBeNull();
    expect(state.error).toBeNull();
  });

  it('fetchAll sets an error when a core call fails', async () => {
    vi.mocked(api.getUsageSummary).mockRejectedValueOnce(new Error('boom'));
    vi.mocked(api.getUsageByProvider).mockResolvedValue([]);
    vi.mocked(api.getUsageByModel).mockResolvedValue([]);
    vi.mocked(api.getUsageByFunction).mockResolvedValue([]);
    vi.mocked(api.getUsageTimeseries).mockResolvedValue([]);
    vi.mocked(api.getUsageBudget).mockResolvedValue(budget);

    await useUsageStore.getState().fetchAll();

    expect(useUsageStore.getState().error).toBe('Failed to load usage data');
    expect(useUsageStore.getState().isLoading).toBe(false);
  });

  it('updateBudget saves and refetches the budget', async () => {
    vi.mocked(api.putUsageBudget).mockResolvedValueOnce(undefined);
    vi.mocked(api.getUsageBudget).mockResolvedValueOnce({ ...budget, max_tokens: 2000 });

    await useUsageStore.getState().updateBudget({ max_tokens: 2000 });

    expect(api.putUsageBudget).toHaveBeenCalledWith({ max_tokens: 2000 });
    expect(useUsageStore.getState().budget?.max_tokens).toBe(2000);
    expect(addToastMock).toHaveBeenCalledWith('Budget saved', 'success');
  });

  it('updateBudget toasts on failure', async () => {
    vi.mocked(api.putUsageBudget).mockRejectedValueOnce(new Error('nope'));

    await useUsageStore.getState().updateBudget({ max_tokens: 2000 });

    expect(addToastMock).toHaveBeenCalledWith('Failed to save budget', 'error');
  });
});
