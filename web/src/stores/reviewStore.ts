import { create } from 'zustand';
import { getReviewQueue } from '../api/client';
import type { ReviewItem } from '../api/types';

interface ReviewStats {
  reviewed: number;
  linked: number;
  tagged: number;
  moved: number;
}

interface ReviewStore {
  queue: ReviewItem[];
  stats: ReviewStats;
  isLoading: boolean;
  error: string | null;
  fetchQueue: () => Promise<void>;
  dismissItem: (noteId: string) => void;
  recordAction: (type: 'linked' | 'tagged' | 'moved') => void;
}

export const useReviewStore = create<ReviewStore>((set) => ({
  queue: [],
  stats: { reviewed: 0, linked: 0, tagged: 0, moved: 0 },
  isLoading: false,
  error: null,

  fetchQueue: async () => {
    set({ isLoading: true, error: null });
    try {
      const items = await getReviewQueue();
      set({ queue: items, isLoading: false });
    } catch {
      set({ error: 'Failed to load review queue', isLoading: false });
    }
  },

  dismissItem: (noteId) =>
    set((state) => ({
      queue: state.queue.filter((item) => item.note_id !== noteId),
      stats: { ...state.stats, reviewed: state.stats.reviewed + 1 },
    })),

  recordAction: (type) =>
    set((state) => ({
      stats: {
        ...state.stats,
        [type]: state.stats[type] + 1,
        reviewed: state.stats.reviewed + 1,
      },
    })),
}));
