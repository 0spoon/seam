import { create } from 'zustand';
import type { Note, NoteFilter, CreateNoteReq, UpdateNoteReq } from '../api/types';
import * as api from '../api/client';

interface NoteState {
  notes: Note[];
  total: number;
  currentNote: Note | null;
  backlinks: Note[];
  isLoading: boolean;
  error: string | null;

  fetchNotes: (filter?: NoteFilter) => Promise<void>;
  fetchNote: (id: string) => Promise<void>;
  createNote: (req: CreateNoteReq) => Promise<Note>;
  updateNote: (id: string, req: UpdateNoteReq) => Promise<Note>;
  deleteNote: (id: string) => Promise<void>;
  fetchBacklinks: (noteId: string) => Promise<void>;
  clearCurrentNote: () => void;
  clearError: () => void;
}

export const useNoteStore = create<NoteState>((set) => ({
  notes: [],
  total: 0,
  currentNote: null,
  backlinks: [],
  isLoading: false,
  error: null,

  fetchNotes: async (filter?: NoteFilter) => {
    set({ isLoading: true, error: null });
    try {
      const { notes, total } = await api.listNotes(filter);
      set({ notes, total, isLoading: false });
    } catch (err) {
      const message = err instanceof api.ApiError ? err.message : 'Failed to fetch notes';
      set({ error: message, isLoading: false });
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
    }
  },

  createNote: async (req: CreateNoteReq) => {
    const note = await api.createNote(req);
    set((state) => ({ notes: [note, ...state.notes], total: state.total + 1 }));
    return note;
  },

  updateNote: async (id: string, req: UpdateNoteReq) => {
    const updated = await api.updateNote(id, req);
    set((state) => ({
      notes: state.notes.map((n) => (n.id === id ? updated : n)),
      currentNote: state.currentNote?.id === id ? updated : state.currentNote,
    }));
    return updated;
  },

  deleteNote: async (id: string) => {
    await api.deleteNote(id);
    set((state) => ({
      notes: state.notes.filter((n) => n.id !== id),
      total: state.total - 1,
      currentNote: state.currentNote?.id === id ? null : state.currentNote,
    }));
  },

  fetchBacklinks: async (noteId: string) => {
    try {
      const backlinks = await api.getBacklinks(noteId);
      set({ backlinks });
    } catch {
      set({ backlinks: [] });
    }
  },

  clearCurrentNote: () => set({ currentNote: null, backlinks: [] }),
  clearError: () => set({ error: null }),
}));
