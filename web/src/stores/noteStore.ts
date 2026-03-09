import { create } from 'zustand';
import type { Note, NoteFilter, CreateNoteReq, UpdateNoteReq } from '../api/types';
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
}

export const useNoteStore = create<NoteState>((set, get) => ({
  notes: [],
  total: 0,
  currentNote: null,
  backlinks: [],
  isLoading: false,
  error: null,
  lastFilter: undefined,

  fetchNotes: async (filter?: NoteFilter) => {
    set({ isLoading: true, error: null, lastFilter: filter });
    try {
      const { notes, total } = await api.listNotes(filter);
      set({ notes, total, isLoading: false });
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
}));
