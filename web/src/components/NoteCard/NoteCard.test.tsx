import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { NoteCard } from './NoteCard';
import type { Note } from '../../api/types';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

const makeNote = (overrides: Partial<Note> = {}): Note => ({
  id: 'note1',
  title: 'Test Note Title',
  body: 'This is the body of the test note with some content to preview.',
  file_path: 'test-note-title.md',
  tags: ['architecture', 'design'],
  created_at: '2026-03-01T10:00:00Z',
  updated_at: '2026-03-08T14:30:00Z',
  ...overrides,
});

function renderNoteCard(note: Note, props: { projectName?: string; projectColor?: string } = {}) {
  return render(
    <MemoryRouter>
      <NoteCard note={note} {...props} />
    </MemoryRouter>,
  );
}

describe('NoteCard', () => {
  it('renders the note title', () => {
    renderNoteCard(makeNote());
    expect(screen.getByText('Test Note Title')).toBeInTheDocument();
  });

  it('renders the body preview', () => {
    renderNoteCard(makeNote());
    expect(screen.getByText(/This is the body of the test note/)).toBeInTheDocument();
  });

  it('renders tags as pills', () => {
    renderNoteCard(makeNote());
    expect(screen.getByText('#architecture')).toBeInTheDocument();
    expect(screen.getByText('#design')).toBeInTheDocument();
  });

  it('renders project name when provided', () => {
    renderNoteCard(makeNote(), {
      projectName: 'My Project',
      projectColor: '#c4915c',
    });
    expect(screen.getByText('My Project')).toBeInTheDocument();
  });

  it('navigates to note on click', () => {
    renderNoteCard(makeNote());
    fireEvent.click(screen.getByRole('listitem'));
    expect(mockNavigate).toHaveBeenCalledWith('/notes/note1');
  });

  it('navigates to note on Enter key', () => {
    renderNoteCard(makeNote());
    fireEvent.keyDown(screen.getByRole('listitem'), { key: 'Enter' });
    expect(mockNavigate).toHaveBeenCalledWith('/notes/note1');
  });

  it('strips markdown from preview', () => {
    renderNoteCard(
      makeNote({
        body: '# Heading\n\nSome **bold** and *italic* text with [[wikilink]] and `code`.',
      }),
    );
    // Should not contain raw markdown markers
    const preview = screen.getByText(/Some bold and italic text/);
    expect(preview.textContent).not.toContain('**');
    expect(preview.textContent).not.toContain('[[');
    expect(preview.textContent).not.toContain('`');
  });

  it('handles notes with no tags', () => {
    renderNoteCard(makeNote({ tags: [] }));
    expect(screen.getByText('Test Note Title')).toBeInTheDocument();
  });

  it('handles notes with empty body', () => {
    renderNoteCard(makeNote({ body: '' }));
    expect(screen.getByText('Test Note Title')).toBeInTheDocument();
  });
});
