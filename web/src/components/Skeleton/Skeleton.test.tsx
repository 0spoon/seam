import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  Skeleton,
  NoteCardSkeleton,
  NoteListSkeleton,
  EditorSkeleton,
  FullPageSkeleton,
  GenericPageSkeleton,
  SearchResultSkeleton,
} from './Skeleton';

describe('Skeleton', () => {
  it('renders with custom dimensions', () => {
    const { container } = render(<Skeleton width="200px" height={40} />);
    const el = container.firstElementChild as HTMLElement;
    expect(el.style.width).toBe('200px');
    expect(el.style.height).toBe('40px');
    expect(el).toHaveAttribute('aria-hidden', 'true');
  });
});

describe('NoteCardSkeleton', () => {
  it('renders with aria-hidden', () => {
    const { container } = render(<NoteCardSkeleton />);
    const el = container.firstElementChild as HTMLElement;
    expect(el).toHaveAttribute('aria-hidden', 'true');
  });
});

describe('NoteListSkeleton', () => {
  it('renders correct number of cards', () => {
    render(<NoteListSkeleton count={3} />);
    const container = screen.getByRole('status', { name: 'Loading notes' });
    expect(container.children).toHaveLength(3);
  });
});

describe('EditorSkeleton', () => {
  it('renders with loading status', () => {
    render(<EditorSkeleton />);
    expect(screen.getByRole('status', { name: 'Loading editor' })).toBeInTheDocument();
  });
});

describe('FullPageSkeleton', () => {
  it('renders', () => {
    render(<FullPageSkeleton />);
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument();
  });
});

describe('GenericPageSkeleton', () => {
  it('renders', () => {
    render(<GenericPageSkeleton />);
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument();
  });
});

describe('SearchResultSkeleton', () => {
  it('renders correct count', () => {
    render(<SearchResultSkeleton count={6} />);
    const container = screen.getByRole('status', { name: 'Loading results' });
    expect(container.children).toHaveLength(6);
  });
});
