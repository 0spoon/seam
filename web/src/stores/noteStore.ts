import { create } from 'zustand';
import type { Note, NoteFilter, CreateNoteReq, UpdateNoteReq, BulkActionResult } from '../api/types';
import * as api from '../api/client';
import { useToastStore } from '../components/Toast/ToastContainer';

interface NoteState {
  notes: Note[];
  total: number;
  currentNote: Note | null;
  backlinks: Note[];
  isLoading: boolean;
  error: string | null;
  // The last filter used by fetchNotes, kept so WS handlers can refetch
  // the list with the same parameters after a remote change.
  lastFilter: NoteFilter | undefined;

  // Selection state for bulk operations.
  selectedNoteIds: Set<string>;
  isSelectionMode: boolean;

  fetchNotes: (filter?: NoteFilter) => Promise<void>;
  fetchNote: (id: string) => Promise<void>;
  createNote: (req: CreateNoteReq) => Promise<Note>;
  updateNote: (id: string, req: UpdateNoteReq) => Promise<Note>;
  deleteNote: (id: string) => Promise<void>;
  fetchBacklinks: (noteId: string) => Promise<void>;
  handleNoteChanged: (noteId: string) => Promise<void>;
  fetchOrCreateDaily: (date?: string) => Promise<Note | null>;
  appendToNote: (noteId: string, text: string) => Promise<Note | null>;
  clearCurrentNote: () => void;
  clearError: () => void;

  // Bulk operation actions.
  toggleNoteSelection: (id: string) => void;
  selectAll: (ids: string[]) => void;
  clearSelection: () => void;
  bulkAction: (action: string, params?: Record<string, string>) => Promise<BulkActionResult | null>;
}

export const useNoteStore = create<NoteState>((set, get) => ({
  notes: [],
  total: 0,
  currentNote: null,
  backlinks: [],
  isLoading: false,
  error: null,
  lastFilter: undefined,
  selectedNoteIds: new Set<string>(),
  isSelectionMode: false,

  fetchNotes: async (filter?: NoteFilter) => {
    set({ isLoading: true, error: null, lastFilter: filter });
    try {
      const { notes, total } = await api.listNotes(filter);
      // When loading with an offset (pagination), append to existing notes
      // instead of replacing them. This prevents other consumers of the store
      // from seeing only the latest page.
      if (filter?.offset && filter.offset > 0) {
        set((state) => {
          const existingIds = new Set(state.notes.map((n) => n.id));
          const newNotes = notes.filter((n) => !existingIds.has(n.id));
          return { notes: [...state.notes, ...newNotes], total, isLoading: false };
        });
      } else {
        set({ notes, total, isLoading: false });
      }
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch notes';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
    }
  },

  fetchNote: async (id: string) => {
    set({ isLoading: true, error: null });
    try {
      const note = await api.getNote(id);
      set({ currentNote: note, isLoading: false });
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch note';
      set({ error: message, isLoading: false });
      useToastStore.getState().addToast(message, 'error');
    }
  },

  createNote: async (req: CreateNoteReq) => {
    try {
      const note = await api.createNote(req);
      // Refetch the list to respect the current sort order instead of
      // blindly prepending, which would be wrong for non-default sorts.
      const { lastFilter } = get();
      const { notes, total } = await api.listNotes(lastFilter);
      set({ notes, total });
      useToastStore.getState().addToast('Note created', 'success');
      return note;
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to create note';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  updateNote: async (id: string, req: UpdateNoteReq) => {
    try {
      const updated = await api.updateNote(id, req);
      set((state) => ({
        notes: state.notes.map((n) => (n.id === id ? updated : n)),
        currentNote: state.currentNote?.id === id ? updated : state.currentNote,
      }));
      return updated;
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to update note';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  deleteNote: async (id: string) => {
    try {
      await api.deleteNote(id);
      set((state) => ({
        notes: state.notes.filter((n) => n.id !== id),
        total: state.total - 1,
        currentNote: state.currentNote?.id === id ? null : state.currentNote,
      }));
      useToastStore.getState().addToast('Note deleted', 'success');
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to delete note';
      set({ error: message });
      useToastStore.getState().addToast(message, 'error');
      throw err;
    }
  },

  fetchBacklinks: async (noteId: string) => {
    try {
      const backlinks = await api.getBacklinks(noteId);
      set({ backlinks });
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch backlinks';
      console.error('fetchBacklinks:', message, err);
      set({ backlinks: [] });
    }
  },

  // Called when a WebSocket `note.changed` event is received.
  // Updates the affected note in the list and refreshes currentNote if it
  // matches.
  handleNoteChanged: async (noteId: string) => {
    try {
      const updated = await api.getNote(noteId);
      set((state) => ({
        notes: state.notes.map((n) => (n.id === noteId ? updated : n)),
        currentNote: state.currentNote?.id === noteId ? updated : state.currentNote,
      }));
    } catch {
      // Note may have been deleted; refetch the full list.
      const { lastFilter } = get();
      try {
        const { notes, total } = await api.listNotes(lastFilter);
        set({ notes, total });
      } catch {
        // Silently ignore -- the next user action will retry.
      }
    }
  },

  fetchOrCreateDaily: async (date?: string) => {
    try {
      const note = await api.getDailyNote(date);
      set({ currentNote: note });
      return note;
    } catch (err) {
      const msg = err instanceof api.ApiError ? err.message : 'Failed to load daily note';
      set({ error: msg });
      useToastStore.getState().addToast(msg, 'error');
      return null;
    }
  },

  appendToNote: async (noteId: string, text: string) => {
    try {
      const note = await api.appendToNote(noteId, text);
      set((state) => ({
        notes: state.notes.map((n) => (n.id === noteId ? note : n)),
        currentNote: state.currentNote?.id === noteId ? note : state.currentNote,
      }));
      return note;
    } catch (err) {
      const msg = err instanceof api.ApiError ? err.message : 'Failed to append to note';
      set({ error: msg });
      useToastStore.getState().addToast(msg, 'error');
      return null;
    }
  },

  clearCurrentNote: () => set({ currentNote: null, backlinks: [] }),
  clearError: () => set({ error: null }),

  toggleNoteSelection: (id: string) => {
    set((state) => {
      const newSet = new Set(state.selectedNoteIds);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return {
        selectedNoteIds: newSet,
        isSelectionMode: newSet.size > 0,
      };
    });
  },

  selectAll: (ids: string[]) => {
    set({ selectedNoteIds: new Set(ids), isSelectionMode: ids.length > 0 });
  },

  clearSelection: () => {
    set({ selectedNoteIds: new Set<string>(), isSelectionMode: false });
  },

  bulkAction: async (action: string, params: Record<string, string> = {}) => {
    const { selectedNoteIds, lastFilter } = get();
    if (selectedNoteIds.size === 0) return null;

    try {
      const result = await api.bulkUpdateNotes(
        Array.from(selectedNoteIds),
        action,
        params,
      );
      // Clear selection and refresh the note list.
      set({ selectedNoteIds: new Set<string>(), isSelectionMode: false });
      const { notes, total } = await api.listNotes(lastFilter);
      set({ notes, total });
      return result;
    } catch (err) {
      const msg = err instanceof api.ApiError ? err.message : 'Bulk action failed';
      set({ error: msg });
      useToastStore.getState().addToast(msg, 'error');
      return null;
    }
  },
}));
