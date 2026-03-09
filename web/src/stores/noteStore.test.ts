import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useNoteStore } from './noteStore';
import type { Note } from '../api/types';

const makeNote = (overrides: Partial<Note> = {}): Note => ({
  id: 'note1',
  title: 'Test Note',
  body: 'Hello world',
  file_path: 'test.md',
  tags: ['tag1'],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  ...overrides,
});

beforeEach(() => {
  useNoteStore.setState({
    notes: [],
    total: 0,
    currentNote: null,
    backlinks: [],
    isLoading: false,
    error: null,
  });
  vi.restoreAllMocks();
});

describe('noteStore', () => {
  it('has correct initial state', () => {
    const state = useNoteStore.getState();
    expect(state.notes).toEqual([]);
    expect(state.total).toBe(0);
    expect(state.currentNote).toBeNull();
    expect(state.isLoading).toBe(false);
  });

  it('fetchNotes populates the notes array', async () => {
    const mockNotes = [makeNote(), makeNote({ id: 'note2', title: 'Note 2' })];

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockNotes), {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'X-Total-Count': '2',
        },
      }),
    );

    await useNoteStore.getState().fetchNotes();

    const state = useNoteStore.getState();
    expect(state.notes).toHaveLength(2);
    expect(state.total).toBe(2);
    expect(state.isLoading).toBe(false);
  });

  it('fetchNote sets currentNote', async () => {
    const mockNote = makeNote();

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(mockNote), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useNoteStore.getState().fetchNote('note1');

    expect(useNoteStore.getState().currentNote?.id).toBe('note1');
  });

  it('createNote adds note to array', async () => {
    const mockNote = makeNote();

    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
      // createNote now refetches the list via listNotes after creation.
      .mockResolvedValueOnce(
        new Response(JSON.stringify([mockNote]), {
          status: 200,
          headers: {
            'Content-Type': 'application/json',
            'X-Total-Count': '1',
          },
        }),
      );

    const result = await useNoteStore.getState().createNote({
      title: 'Test Note',
      body: 'Hello world',
    });

    expect(result.id).toBe('note1');
    expect(useNoteStore.getState().notes).toHaveLength(1);
    expect(useNoteStore.getState().total).toBe(1);
  });

  it('deleteNote removes note from array', async () => {
    useNoteStore.setState({
      notes: [makeNote(), makeNote({ id: 'note2' })],
      total: 2,
    });

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    await useNoteStore.getState().deleteNote('note1');

    const state = useNoteStore.getState();
    expect(state.notes).toHaveLength(1);
    expect(state.notes[0].id).toBe('note2');
    expect(state.total).toBe(1);
  });

  it('updateNote updates note in array and currentNote', async () => {
    const original = makeNote();
    useNoteStore.setState({
      notes: [original],
      currentNote: original,
    });

    const updated = { ...original, title: 'Updated Title' };

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify(updated), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await useNoteStore.getState().updateNote('note1', { title: 'Updated Title' });

    expect(useNoteStore.getState().notes[0].title).toBe('Updated Title');
    expect(useNoteStore.getState().currentNote?.title).toBe('Updated Title');
  });

  it('clearCurrentNote resets currentNote and backlinks', () => {
    useNoteStore.setState({
      currentNote: makeNote(),
      backlinks: [makeNote({ id: 'bl1' })],
    });

    useNoteStore.getState().clearCurrentNote();

    expect(useNoteStore.getState().currentNote).toBeNull();
    expect(useNoteStore.getState().backlinks).toEqual([]);
  });
});
