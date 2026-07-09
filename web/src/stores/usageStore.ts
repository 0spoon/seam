import { create } from 'zustand';
import * as api from '../api/client';
import type {
  UsageSummary,
  ProviderUsage,
  ModelUsage,
  FunctionUsage,
  TimeSeriesPoint,
  UsageBudget,
  UsageBudgetUpdate,
  RetrievalSummary,
} from '../api/types';
import { useToastStore } from '../components/Toast/ToastContainer';

interface UsageStore {
  summary: UsageSummary | null;
  byProvider: ProviderUsage[];
  byModel: ModelUsage[];
  byFunction: FunctionUsage[];
  timeseries: TimeSeriesPoint[];
  budget: UsageBudget | null;
  retrieval: RetrievalSummary | null;
  isLoading: boolean;
  error: string | null;

  fetchAll: () => Promise<void>;
  updateBudget: (update: UsageBudgetUpdate) => Promise<void>;
}

export const useUsageStore = create<UsageStore>((set) => ({
  summary: null,
  byProvider: [],
  byModel: [],
  byFunction: [],
  timeseries: [],
  budget: null,
  retrieval: null,
  isLoading: false,
  error: null,

  fetchAll: async () => {
    set({ isLoading: true, error: null });
    try {
      const [summary, byProvider, byModel, byFunction, timeseries, budget] = await Promise.all([
        api.getUsageSummary(),
        api.getUsageByProvider(),
        api.getUsageByModel(),
        api.getUsageByFunction(),
        api.getUsageTimeseries(),
        api.getUsageBudget(),
      ]);
      // Retrieval telemetry is optional (endpoint 404s when disabled); fetch it
      // separately so its absence does not blank the whole dashboard.
      let retrieval: RetrievalSummary | null = null;
      try {
        retrieval = await api.getRetrievalSummary();
      } catch {
        retrieval = null;
      }
      set({
        summary,
        byProvider,
        byModel,
        byFunction,
        timeseries,
        budget,
        retrieval,
        isLoading: false,
      });
    } catch {
      set({ error: 'Failed to load usage data', isLoading: false });
    }
  },

  updateBudget: async (update) => {
    try {
      await api.putUsageBudget(update);
      const budget = await api.getUsageBudget();
      set({ budget });
      useToastStore.getState().addToast('Budget saved', 'success');
    } catch {
      useToastStore.getState().addToast('Failed to save budget', 'error');
    }
  },
}));
