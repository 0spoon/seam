import { describe, it, expect, beforeEach, vi } from 'vitest';
import { addRecentNote, getRecentNotes, clearRecentNotes } from './recentNotes';

describe('recentNotes', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns empty array when no recent notes', () => {
    expect(getRecentNotes()).toEqual([]);
  });

  it('adds a recent note', () => {
    addRecentNote('n1', 'My Note');
    const notes = getRecentNotes();
    expect(notes).toHaveLength(1);
    expect(notes[0].id).toBe('n1');
    expect(notes[0].title).toBe('My Note');
    expect(notes[0].openedAt).toBeGreaterThan(0);
  });

  it('deduplicates by ID and moves to front', () => {
    addRecentNote('n1', 'First');
    addRecentNote('n2', 'Second');
    addRecentNote('n1', 'First Updated');

    const notes = getRecentNotes();
    expect(notes).toHaveLength(2);
    expect(notes[0].id).toBe('n1');
    expect(notes[0].title).toBe('First Updated');
    expect(notes[1].id).toBe('n2');
  });

  it('trims to MAX_RECENT (10)', () => {
    for (let i = 0; i < 15; i++) {
      addRecentNote(`n${i}`, `Note ${i}`);
    }
    const notes = getRecentNotes();
    expect(notes).toHaveLength(10);
    // Most recent should be first
    expect(notes[0].id).toBe('n14');
  });

  it('clears all recent notes', () => {
    addRecentNote('n1', 'Test');
    clearRecentNotes();
    expect(getRecentNotes()).toEqual([]);
  });

  it('handles corrupted localStorage gracefully', () => {
    localStorage.setItem('seam_recent_notes', 'not-valid-json');
    expect(getRecentNotes()).toEqual([]);
  });

  it('handles non-array localStorage gracefully', () => {
    localStorage.setItem('seam_recent_notes', '"just a string"');
    expect(getRecentNotes()).toEqual([]);
  });
});
