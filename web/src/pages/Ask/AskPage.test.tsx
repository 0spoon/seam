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
  createConversation: vi.fn(),
  listConversations: vi.fn(),
  getConversation: vi.fn(),
  deleteConversation: vi.fn(),
  streamAssistantChat: vi.fn(),
  streamResumeAction: vi.fn(),
  rejectAssistantAction: vi.fn(),
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
  getConversation,
  streamAssistantChat,
  streamResumeAction,
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
    // Default: keep the stream open so the page sits in the streaming
    // state. Tests that need a completed stream can override this with
    // a mockImplementationOnce that calls the onEvent callback.
    vi.mocked(streamAssistantChat).mockImplementation(
      () => new Promise(() => {}),
    );

    // jsdom does not implement scrollIntoView.
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders title and subtitle', async () => {
    await renderAskPage();
    expect(screen.getByText('Ask Seam')).toBeInTheDocument();
    // The subtitle text was rewritten when the page moved to the
    // agentic assistant. Match a substring instead of the full string
    // so future copy tweaks don't break this test.
    expect(
      screen.getByText(/assistant can search, read, create/i),
    ).toBeInTheDocument();
  });

  it('shows empty state when no messages', async () => {
    await renderAskPage();
    expect(
      screen.getByText(/Ask anything.*Seam finds the answer/),
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
      screen.getByText(/Ask anything.*Seam finds the answer/),
    ).toBeInTheDocument();
  });

  it('does not submit empty input', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    const form = input.closest('form')!;
    fireEvent.submit(form);

    // Empty state should still be showing.
    expect(
      screen.getByText(/Ask anything.*Seam finds the answer/),
    ).toBeInTheDocument();
  });

  it('hides empty state after first message', async () => {
    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'hello' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(
        screen.queryByText(/Ask anything.*Seam finds the answer/),
      ).not.toBeInTheDocument();
    });
  });

  it('handleApprove streams resume events and reloads conversation', async () => {
    // streamAssistantChat fires a confirmation event so the page enters
    // awaiting_approval. The resume stream then emits tool_use + done.
    vi.mocked(streamAssistantChat).mockImplementationOnce(
      async (_convId, _msg, _hist, onEvent) => {
        onEvent({
          type: 'confirmation',
          tool_name: 'create_note',
          content: 'act_pending_1',
        });
        onEvent({ type: 'done' });
      },
    );
    vi.mocked(streamResumeAction).mockImplementationOnce(
      async (_actionId, onEvent) => {
        onEvent({
          type: 'tool_use',
          tool_name: 'create_note',
          content: '{"id":"n1"}',
        });
        onEvent({ type: 'text', content: 'Note created.' });
        onEvent({ type: 'done' });
      },
    );
    vi.mocked(getConversation).mockResolvedValue({
      id: 'conv1',
      title: '',
      messages: [
        {
          id: 'm1',
          conversation_id: 'conv1',
          role: 'user',
          content: 'create a note',
          created_at: '',
        },
        {
          id: 'm2',
          conversation_id: 'conv1',
          role: 'assistant',
          content: 'Note created.',
          created_at: '',
        },
      ],
    } as never);

    await renderAskPage();

    const input = screen.getByLabelText('Ask a question');
    fireEvent.change(input, { target: { value: 'create a note' } });
    fireEvent.submit(input.closest('form')!);

    // Wait for the confirmation card to appear.
    const approveBtn = await screen.findByRole('button', { name: /approve/i });
    fireEvent.click(approveBtn);

    // After resume completes, getConversation is called and the
    // canonical assistant text becomes visible.
    await waitFor(() => {
      expect(getConversation).toHaveBeenCalledWith('conv1');
    });
    await waitFor(() => {
      expect(screen.getAllByText('Note created.').length).toBeGreaterThan(0);
    });
    expect(streamResumeAction).toHaveBeenCalledWith(
      'act_pending_1',
      expect.any(Function),
      expect.any(AbortSignal),
    );
  });
});
