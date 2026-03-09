import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AskPage } from './AskPage';

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('../../api/client', () => ({
  askSeam: vi.fn(),
}));

vi.mock('../../api/ws', () => ({
  send: vi.fn(),
}));

vi.mock('../../hooks/useWebSocket', () => ({
  useWebSocket: vi.fn(),
}));

vi.mock('../../lib/markdown', () => ({
  renderMarkdown: (s: string) => `<p>${s}</p>`,
}));

import { askSeam } from '../../api/client';

function renderAskPage() {
  return render(
    <MemoryRouter>
      <AskPage />
    </MemoryRouter>,
  );
}

describe('AskPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // jsdom does not implement scrollIntoView.
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders title and subtitle', () => {
    renderAskPage();
    expect(screen.getByText('Ask Seam')).toBeInTheDocument();
    expect(
      screen.getByText(/Ask questions about your notes/),
    ).toBeInTheDocument();
  });

  it('shows empty state when no messages', () => {
    renderAskPage();
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('has a text input with correct placeholder', () => {
    renderAskPage();
    expect(
      screen.getByPlaceholderText('Ask about your notes...'),
    ).toBeInTheDocument();
  });

  it('send button is disabled when input is empty', () => {
    renderAskPage();
    const sendButton = screen.getByLabelText('Send');
    expect(sendButton).toBeDisabled();
  });

  it('send button is enabled when input has text', () => {
    renderAskPage();
    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'What is caching?' } });
    expect(screen.getByLabelText('Send')).not.toBeDisabled();
  });

  it('adds user message on submit', async () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'What is caching?' } });

    const form = input.closest('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByText('What is caching?')).toBeInTheDocument();
    });
  });

  it('shows thinking state during streaming', async () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test question' } });

    const form = input.closest('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByText('Thinking...')).toBeInTheDocument();
    });
  });

  it('disables input while streaming', async () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test' } });

    const form = input.closest('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByLabelText('Ask a question')).toBeDisabled();
    });
  });

  it('clears input after submit', async () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'my question' } });

    const form = input.closest('form')!;
    fireEvent.submit(form);

    await waitFor(() => {
      expect(input.value).toBe('');
    });
  });

  it('does not submit on Shift+Enter', () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });

    // No user message should appear - empty state still showing.
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('does not submit empty input', () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    const form = input.closest('form')!;
    fireEvent.submit(form);

    // Empty state should still be showing.
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('hides empty state after first message', async () => {
    renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'hello' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(
        screen.queryByText(/Ask a question and Seam will search your notes/),
      ).not.toBeInTheDocument();
    });
  });
});
