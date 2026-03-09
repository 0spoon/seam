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
  createConversation: vi.fn(),
  listConversations: vi.fn(),
  getConversation: vi.fn(),
  addChatMessage: vi.fn(),
  deleteConversation: vi.fn(),
}));

vi.mock('../../api/ws', () => ({
  send: vi.fn(),
  isConnected: vi.fn(() => true),
}));

vi.mock('../../hooks/useWebSocket', () => ({
  useWebSocket: vi.fn(),
}));

vi.mock('../../lib/markdown', () => ({
  renderMarkdown: (s: string) => `<p>${s}</p>`,
}));

vi.mock('../../lib/sanitize', () => ({
  sanitizeHtml: (s: string) => s,
}));

import {
  createConversation,
  listConversations,
  addChatMessage,
} from '../../api/client';

/**
 * Renders the AskPage and waits for the initial conversation loading to
 * finish (listConversations resolves, isLoading becomes false).
 */
async function renderAskPage() {
  render(
    <MemoryRouter>
      <AskPage />
    </MemoryRouter>,
  );
  // The component starts with isLoading=true and fetches conversations on
  // mount. Wait for loading to complete so the full UI is available.
  await waitFor(() => {
    expect(screen.queryByText('Loading conversation...')).not.toBeInTheDocument();
  });
}

describe('AskPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Re-establish mock return values after clearAllMocks wipes them.
    vi.mocked(listConversations).mockResolvedValue({
      conversations: [],
      total: 0,
    } as never);
    vi.mocked(createConversation).mockResolvedValue({
      id: 'conv1',
      title: '',
      created_at: '',
      updated_at: '',
    } as never);
    vi.mocked(addChatMessage).mockResolvedValue(undefined as never);

    // jsdom does not implement scrollIntoView.
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders title and subtitle', async () => {
    await renderAskPage();
    expect(screen.getByText('Ask Seam')).toBeInTheDocument();
    expect(
      screen.getByText(/Ask questions about your notes/),
    ).toBeInTheDocument();
  });

  it('shows empty state when no messages', async () => {
    await renderAskPage();
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('has a text input with correct placeholder', async () => {
    await renderAskPage();
    expect(
      screen.getByPlaceholderText('Ask about your notes...'),
    ).toBeInTheDocument();
  });

  it('send button is disabled when input is empty', async () => {
    await renderAskPage();
    const sendButton = screen.getByLabelText('Send');
    expect(sendButton).toBeDisabled();
  });

  it('send button is enabled when input has text', async () => {
    await renderAskPage();
    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'What is caching?' } });
    expect(screen.getByLabelText('Send')).not.toBeDisabled();
  });

  it('adds user message on submit', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'What is caching?' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      // The user message should appear in a message div (not the textarea).
      const matches = screen.getAllByText('What is caching?');
      const messageEl = matches.find((el) => el.closest('[class*="message"]'));
      expect(messageEl).toBeTruthy();
    });
  });

  it('shows thinking state during streaming', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test question' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(screen.getByText('Thinking...')).toBeInTheDocument();
    });
  });

  it('disables input while streaming', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(screen.getByLabelText('Ask a question')).toBeDisabled();
    });
  });

  it('clears input after submit', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'my question' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(input.value).toBe('');
    });
  });

  it('does not submit on Shift+Enter', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'test' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });

    // No user message should appear - empty state still showing.
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('does not submit empty input', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    const form = input.closest('form')!;
    fireEvent.submit(form);

    // Empty state should still be showing.
    expect(
      screen.getByText(/Ask a question and Seam will search your notes/),
    ).toBeInTheDocument();
  });

  it('hides empty state after first message', async () => {
    await renderAskPage();

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
