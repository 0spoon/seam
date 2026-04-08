import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { EmptyState } from './EmptyState';

describe('EmptyState', () => {
  it('renders heading and subtext', () => {
    render(<EmptyState heading="No items" subtext="Nothing to see here" />);
    expect(screen.getByText('No items')).toBeInTheDocument();
    expect(screen.getByText('Nothing to see here')).toBeInTheDocument();
  });

  it('renders action button when provided', () => {
    const onClick = vi.fn();
    render(
      <EmptyState heading="Empty" subtext="Click below" action={{ label: 'Add item', onClick }} />,
    );
    const button = screen.getByText('Add item');
    expect(button).toBeInTheDocument();
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('does not render action button when not provided', () => {
    render(<EmptyState heading="Empty" subtext="No action" />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
