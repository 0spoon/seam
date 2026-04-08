import { useState, useRef, useEffect, useCallback, useMemo } from 'react';
import { Send, Loader2, Plus, Trash2, ChevronDown, Square } from 'lucide-react';
import {
  createConversation,
  listConversations,
  getConversation,
  deleteConversation,
  streamAssistantChat,
  streamResumeAction,
  rejectAssistantAction,
} from '../../api/client';
import { sanitizeHtml } from '../../lib/sanitize';
import { renderMarkdown } from '../../lib/markdown';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { ToolCallCard } from '../../components/ToolCallCard/ToolCallCard';
import { ToolConfirmationCard } from '../../components/ToolConfirmationCard/ToolConfirmationCard';
import type {
  AssistantMessage,
  AssistantStreamEvent,
  ChatHistoryMessage,
  Conversation,
  ToolCallView,
} from '../../api/types';
import styles from './AskPage.module.css';

const STARTER_SUGGESTIONS = [
  'What are my recent notes about?',
  'Find connections between my notes',
  'Summarize my key ideas',
  'What topics do I write about most?',
];

// Maximum number of prior messages to send as history context.
const MAX_HISTORY_MESSAGES = 20;

type StreamStatus = 'idle' | 'streaming' | 'awaiting_approval' | 'error';

interface PendingConfirmation {
  actionId: string;
  toolName: string;
}

// Generate a local short ID for ToolCallView entries. Not a ULID, but the
// field is purely for React keys on client-side cards.
function localId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// toAssistantHistory converts persisted ChatHistoryMessages (as returned
// by getConversation) into the wire-format AssistantMessage shape the
// server expects in the `history` field of /assistant/chat/stream.
//
// system rows are skipped because they're audit markers (e.g., "Pending
// confirmation for X (action Y)") that would confuse the LLM. Every
// other role is passed through unchanged. The server now produces
// well-paired histories (assistant tool_call envelopes followed by
// matching tool result rows), so the client just forwards them.
function toAssistantHistory(
  msgs: ChatHistoryMessage[],
): AssistantMessage[] {
  const out: AssistantMessage[] = [];
  for (const m of msgs) {
    if (m.role === 'system') continue;
    out.push({
      role: m.role,
      content: m.content,
      tool_calls: m.tool_calls,
      tool_call_id: m.tool_call_id,
      tool_name: m.tool_name,
    });
  }
  return out;
}

export function AskPage() {
  const addToast = useToastStore((s) => s.addToast);

  const [input, setInput] = useState('');
  const [messages, setMessages] = useState<ChatHistoryMessage[]>([]);
  const [streamingTools, setStreamingTools] = useState<ToolCallView[]>([]);
  const [streamingText, setStreamingText] = useState('');
  const [streamStatus, setStreamStatus] = useState<StreamStatus>('idle');
  const [pendingConfirmation, setPendingConfirmation] =
    useState<PendingConfirmation | null>(null);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [recentConversations, setRecentConversations] = useState<
    Conversation[]
  >([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showConvDropdown, setShowConvDropdown] = useState(false);
  const [approvalLoading, setApprovalLoading] = useState(false);

  const abortRef = useRef<AbortController | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const convDropdownRef = useRef<HTMLDivElement>(null);

  // Scroll to bottom whenever anything in the visible stream changes.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingText, streamingTools, pendingConfirmation]);

  // Load the most recent conversation on mount.
  useEffect(() => {
    let cancelled = false;
    async function loadRecent() {
      try {
        const { conversations } = await listConversations(10, 0);
        if (cancelled) return;
        setRecentConversations(conversations);
        if (conversations.length > 0) {
          const recent = conversations[0];
          const { conversation, messages: msgs } = await getConversation(
            recent.id,
          );
          if (cancelled) return;
          setConversationId(conversation.id);
          setMessages(msgs);
        }
      } catch {
        // Silently fall through to empty state.
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }
    loadRecent();
    return () => {
      cancelled = true;
    };
  }, []);

  // Close dropdown on outside click.
  useEffect(() => {
    if (!showConvDropdown) return;
    const handleClick = (e: MouseEvent) => {
      if (
        convDropdownRef.current &&
        !convDropdownRef.current.contains(e.target as Node)
      ) {
        setShowConvDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [showConvDropdown]);

  // Focus input once load completes.
  useEffect(() => {
    if (!isLoading) {
      inputRef.current?.focus();
    }
  }, [isLoading]);

  // Esc cancels an in-flight stream.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && streamStatus === 'streaming') {
        abortRef.current?.abort();
        abortRef.current = null;
        setStreamStatus('idle');
        setStreamingText('');
        setStreamingTools([]);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [streamStatus]);

  // Abort any in-flight stream on unmount.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const handleSwitchConversation = useCallback(
    async (convId: string) => {
      setShowConvDropdown(false);
      try {
        const { conversation, messages: msgs } = await getConversation(convId);
        setConversationId(conversation.id);
        setMessages(msgs);
        setStreamingText('');
        setStreamingTools([]);
        setPendingConfirmation(null);
        setStreamStatus('idle');
      } catch {
        addToast('Failed to load conversation', 'error');
      }
    },
    [addToast],
  );

  // Kick off a stream from the current state. The caller is responsible for
  // first pushing any synthetic user/tool messages into `messages`. This
  // builds the history from the snapshot of messages passed in, not the
  // stale closure -- the caller supplies the truth.
  const runStream = useCallback(
    async (convId: string, message: string, baseHistory: ChatHistoryMessage[]) => {
      const history = toAssistantHistory(baseHistory).slice(
        -MAX_HISTORY_MESSAGES,
      );

      const controller = new AbortController();
      abortRef.current = controller;
      setStreamStatus('streaming');
      setStreamingText('');
      setStreamingTools([]);

      // Accumulators for the stream so we can finalize from a single place
      // on `done`. Using refs-via-closures here would lose updates between
      // events because setState batches.
      let finalText = '';
      const finalTools: ToolCallView[] = [];
      let confirmationPending: PendingConfirmation | null = null;

      const handleEvent = (e: AssistantStreamEvent) => {
        if (e.type === 'text') {
          finalText = e.content ?? '';
          setStreamingText(finalText);
        } else if (e.type === 'tool_use') {
          const view: ToolCallView = {
            id: localId(),
            toolName: e.tool_name ?? 'tool',
            status: e.error ? 'error' : 'ok',
            resultJson: e.content,
            errorMessage: e.error,
          };
          finalTools.push(view);
          setStreamingTools((prev) => [...prev, view]);
        } else if (e.type === 'confirmation') {
          confirmationPending = {
            actionId: e.content ?? '',
            toolName: e.tool_name ?? 'tool',
          };
        } else if (e.type === 'done') {
          // Finalize below in the post-stream block.
        } else if (e.type === 'error') {
          const msg = e.error ?? 'Stream error';
          addToast(msg, 'error');
          setStreamStatus('error');
          setStreamingText('');
          setStreamingTools([]);
        }
      };

      try {
        await streamAssistantChat(
          convId,
          message,
          history,
          handleEvent,
          controller.signal,
        );
      } catch (err) {
        if (controller.signal.aborted) {
          // User cancelled -- treat as idle.
          setStreamStatus('idle');
          setStreamingText('');
          setStreamingTools([]);
          return;
        }
        const msg = err instanceof Error ? err.message : 'Request failed';
        addToast(msg, 'error');
        setStreamStatus('error');
        setStreamingText('');
        setStreamingTools([]);
        return;
      } finally {
        if (abortRef.current === controller) {
          abortRef.current = null;
        }
      }

      // Stream ended cleanly (no throw). Finalize by reifying the streaming
      // scratch state into persistent ChatHistoryMessage rows. These are
      // synthetic client-side rows; the canonical rows will arrive on next
      // getConversation reload, which replaces them.
      const synthTools: ChatHistoryMessage[] = finalTools.map((t) => ({
        id: t.id,
        conversation_id: convId,
        role: 'tool',
        content: t.resultJson ?? '',
        tool_name: t.toolName,
        created_at: new Date().toISOString(),
      }));

      const synthAssistant: ChatHistoryMessage | null =
        finalText || finalTools.length > 0
          ? {
              id: localId(),
              conversation_id: convId,
              role: 'assistant',
              content: finalText,
              // Attach a tool_calls envelope for the reload-match logic so
              // the assistant row absorbs any preceding synth tool messages.
              tool_calls:
                finalTools.length > 0
                  ? finalTools.map((t) => ({
                      id: t.id,
                      name: t.toolName,
                      arguments: '',
                    }))
                  : undefined,
              created_at: new Date().toISOString(),
            }
          : null;

      setMessages((prev) => {
        const next = [...prev, ...synthTools];
        if (synthAssistant) next.push(synthAssistant);
        return next;
      });
      setStreamingText('');
      setStreamingTools([]);

      if (confirmationPending) {
        setPendingConfirmation(confirmationPending);
        setStreamStatus('awaiting_approval');
      } else {
        setStreamStatus('idle');
      }
    },
    [addToast],
  );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const query = input.trim();
    if (!query || streamStatus !== 'idle') return;

    let convId = conversationId;
    if (!convId) {
      try {
        const conv = await createConversation();
        convId = conv.id;
        setConversationId(conv.id);
        setRecentConversations((prev) => [conv, ...prev].slice(0, 10));
      } catch {
        addToast('Failed to create conversation', 'error');
        return;
      }
    }

    // Synthetic user message for instant feedback. The server also persists
    // the canonical row; on next reload the canonical row replaces this one.
    const userMsg: ChatHistoryMessage = {
      id: localId(),
      conversation_id: convId,
      role: 'user',
      content: query,
      created_at: new Date().toISOString(),
    };
    const nextMessages = [...messages, userMsg];
    setMessages(nextMessages);
    setInput('');

    await runStream(convId, query, messages);
  };

  const handleApprove = async () => {
    if (!pendingConfirmation || !conversationId) return;
    const actionId = pendingConfirmation.actionId;

    setApprovalLoading(true);
    setPendingConfirmation(null);
    setStreamStatus('streaming');
    setStreamingText('');
    setStreamingTools([]);

    const controller = new AbortController();
    abortRef.current = controller;

    let finalText = '';
    const finalTools: ToolCallView[] = [];
    let nextConfirmation: PendingConfirmation | null = null;

    const handleEvent = (e: AssistantStreamEvent) => {
      if (e.type === 'text') {
        finalText = e.content ?? '';
        setStreamingText(finalText);
      } else if (e.type === 'tool_use') {
        const view: ToolCallView = {
          id: localId(),
          toolName: e.tool_name ?? 'tool',
          status: e.error ? 'error' : 'ok',
          resultJson: e.content,
          errorMessage: e.error,
        };
        finalTools.push(view);
        setStreamingTools((prev) => [...prev, view]);
      } else if (e.type === 'confirmation') {
        nextConfirmation = {
          actionId: e.content ?? '',
          toolName: e.tool_name ?? 'tool',
        };
      } else if (e.type === 'error') {
        const msg = e.error ?? 'Stream error';
        addToast(msg, 'error');
        setStreamStatus('error');
      }
    };

    try {
      await streamResumeAction(actionId, handleEvent, controller.signal);
    } catch (err) {
      if (controller.signal.aborted) {
        setStreamStatus('idle');
        setStreamingText('');
        setStreamingTools([]);
        return;
      }
      const msg = err instanceof Error ? err.message : 'Approval failed';
      addToast(msg, 'error');
      setStreamStatus('error');
      setStreamingText('');
      setStreamingTools([]);
      return;
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
      }
      setApprovalLoading(false);
    }

    // The resume stream completed cleanly. Reload the canonical
    // conversation from the server so the UI reflects what was
    // persisted (assistant envelopes, tool results, final text).
    try {
      const { messages: msgs } = await getConversation(conversationId);
      setMessages(msgs);
    } catch {
      // Non-fatal: the streaming scratch state still shows the result.
    }
    setStreamingText('');
    setStreamingTools([]);

    if (nextConfirmation) {
      setPendingConfirmation(nextConfirmation);
      setStreamStatus('awaiting_approval');
    } else {
      setStreamStatus('idle');
    }
  };

  const handleReject = async () => {
    if (!pendingConfirmation || !conversationId) return;
    setApprovalLoading(true);
    try {
      await rejectAssistantAction(pendingConfirmation.actionId);
      const syntheticSystem: ChatHistoryMessage = {
        id: localId(),
        conversation_id: conversationId,
        role: 'system',
        content: 'Action rejected',
        created_at: new Date().toISOString(),
      };
      setMessages((prev) => [...prev, syntheticSystem]);
      setPendingConfirmation(null);
      setStreamStatus('idle');
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Reject failed';
      addToast(msg, 'error');
    } finally {
      setApprovalLoading(false);
    }
  };

  const handleStop = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setStreamStatus('idle');
    setStreamingText('');
    setStreamingTools([]);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit(e);
    }
  };

  const handleNewConversation = async () => {
    try {
      const conv = await createConversation();
      setConversationId(conv.id);
      setMessages([]);
      setStreamingText('');
      setStreamingTools([]);
      setPendingConfirmation(null);
      setStreamStatus('idle');
      setInput('');
      setRecentConversations((prev) => [conv, ...prev].slice(0, 10));
      inputRef.current?.focus();
    } catch {
      addToast('Failed to create conversation', 'error');
    }
  };

  const handleClearConversation = async () => {
    if (conversationId) {
      try {
        await deleteConversation(conversationId);
      } catch {
        addToast('Failed to delete conversation', 'error');
      }
    }
    setConversationId(null);
    setMessages([]);
    setStreamingText('');
    setStreamingTools([]);
    setPendingConfirmation(null);
    setStreamStatus('idle');
    setInput('');
    inputRef.current?.focus();
  };

  const handleSuggestionClick = (suggestion: string) => {
    setInput(suggestion);
    setTimeout(() => {
      inputRef.current?.form?.requestSubmit();
    }, 0);
  };

  // Build a set of tool_call_ids that have been absorbed into an assistant
  // envelope message, so we can skip rendering the raw `tool` message for
  // them when reconstructing history (avoiding duplicates).
  const toolByCallId = useMemo(() => {
    const map = new Map<string, ChatHistoryMessage>();
    for (const m of messages) {
      if (m.role === 'tool' && m.tool_call_id) {
        map.set(m.tool_call_id, m);
      }
    }
    return map;
  }, [messages]);

  const absorbedToolIds = useMemo(() => {
    const s = new Set<string>();
    for (const m of messages) {
      if (m.role === 'assistant' && m.tool_calls) {
        for (const tc of m.tool_calls) {
          if (tc.id && toolByCallId.has(tc.id)) {
            s.add(tc.id);
          }
        }
      }
    }
    return s;
  }, [messages, toolByCallId]);

  const renderedStreamingText = useMemo(() => {
    if (!streamingText) return '';
    return sanitizeHtml(renderMarkdown(streamingText));
  }, [streamingText]);

  if (isLoading) {
    return (
      <div className={styles.page}>
        <header className={styles.header}>
          <h1 className={styles.title}>Ask Seam</h1>
          <p className={styles.subtitle}>Loading conversation...</p>
        </header>
      </div>
    );
  }

  const isBusy = streamStatus !== 'idle';

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerRow}>
          <div>
            <h1 className={styles.title}>Ask Seam</h1>
            <p className={styles.subtitle}>
              Ask anything. The assistant can search, read, create, and update
              your notes.
            </p>
          </div>
          <div className={styles.headerActions}>
            {recentConversations.length > 1 && (
              <div className={styles.convSwitcher} ref={convDropdownRef}>
                <button
                  className={styles.headerButton}
                  onClick={() => setShowConvDropdown(!showConvDropdown)}
                  title="Recent conversations"
                  aria-label="Recent conversations"
                  aria-expanded={showConvDropdown}
                  aria-haspopup="listbox"
                >
                  <ChevronDown size={14} />
                </button>
                {showConvDropdown && (
                  <div className={styles.convDropdown} role="listbox">
                    {recentConversations.map((conv) => (
                      <button
                        key={conv.id}
                        className={`${styles.convDropdownItem} ${conv.id === conversationId ? styles.convDropdownItemActive : ''}`}
                        onClick={() => handleSwitchConversation(conv.id)}
                        role="option"
                        aria-selected={conv.id === conversationId}
                      >
                        {conv.title || 'New conversation'}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            {messages.length > 0 && (
              <button
                className={styles.headerButton}
                onClick={handleClearConversation}
                title="Clear conversation"
                aria-label="Clear conversation"
              >
                <Trash2 size={14} />
              </button>
            )}
            <button
              className={styles.headerButton}
              onClick={handleNewConversation}
              title="New conversation"
              aria-label="New conversation"
            >
              <Plus size={14} />
            </button>
          </div>
        </div>
      </header>

      <div className={styles.chatArea}>
        {messages.length === 0 && !isBusy && (
          <div className={styles.emptyState}>
            <p className={styles.emptyText}>
              Ask anything -- Seam finds the answer in your notes.
            </p>
            <div className={styles.suggestions}>
              {STARTER_SUGGESTIONS.map((suggestion) => (
                <button
                  key={suggestion}
                  className={styles.suggestionChip}
                  onClick={() => handleSuggestionClick(suggestion)}
                >
                  {suggestion}
                </button>
              ))}
            </div>
          </div>
        )}

        {messages.map((msg) => {
          if (msg.role === 'user') {
            return (
              <div
                key={msg.id}
                className={`${styles.message} ${styles.userMessage}`}
              >
                <div className={styles.messageContent}>{msg.content}</div>
              </div>
            );
          }

          if (msg.role === 'system') {
            return (
              <div key={msg.id} className={styles.systemMessage}>
                {msg.content}
              </div>
            );
          }

          if (msg.role === 'tool') {
            // Skip if this tool message was absorbed into an assistant
            // tool_calls envelope earlier in the message list.
            if (msg.tool_call_id && absorbedToolIds.has(msg.tool_call_id)) {
              return null;
            }
            return (
              <div key={msg.id} className={styles.toolsGroup}>
                <ToolCallCard
                  toolName={msg.tool_name ?? 'tool'}
                  status="ok"
                  resultJson={msg.content}
                />
              </div>
            );
          }

          // assistant
          const hasToolCalls =
            Array.isArray(msg.tool_calls) && msg.tool_calls.length > 0;
          return (
            <div key={msg.id}>
              {hasToolCalls && (
                <div className={styles.toolsGroup}>
                  <p className={styles.toolsLabel}>Used tools:</p>
                  {msg.tool_calls!.map((tc) => {
                    const paired = toolByCallId.get(tc.id);
                    return (
                      <ToolCallCard
                        key={tc.id}
                        toolName={tc.name}
                        status="ok"
                        resultJson={paired?.content}
                      />
                    );
                  })}
                </div>
              )}
              {msg.content && (
                <div
                  className={`${styles.message} ${styles.assistantMessage}`}
                >
                  <div
                    className={styles.messageContent}
                    dangerouslySetInnerHTML={{
                      __html: sanitizeHtml(renderMarkdown(msg.content)),
                    }}
                  />
                </div>
              )}
            </div>
          );
        })}

        <div aria-live="polite" aria-atomic="false">
          {streamingTools.length > 0 && (
            <div className={styles.toolsGroup}>
              {streamingTools.map((t) => (
                <ToolCallCard
                  key={t.id}
                  toolName={t.toolName}
                  status={t.status}
                  resultJson={t.resultJson}
                  errorMessage={t.errorMessage}
                />
              ))}
            </div>
          )}

          {streamingText && (
            <div className={`${styles.message} ${styles.assistantMessage}`}>
              <div
                className={styles.messageContent}
                dangerouslySetInnerHTML={{ __html: renderedStreamingText }}
              />
            </div>
          )}

          {streamStatus === 'streaming' &&
            !streamingText &&
            streamingTools.length === 0 && (
              <div className={`${styles.message} ${styles.assistantMessage}`}>
                <Loader2 size={16} className={styles.spinner} />
                <span className={styles.thinkingText}>Thinking...</span>
              </div>
            )}

          {pendingConfirmation && (
            <ToolConfirmationCard
              toolName={pendingConfirmation.toolName}
              arguments=""
              onApprove={handleApprove}
              onReject={handleReject}
              loading={approvalLoading}
            />
          )}
        </div>

        <div ref={messagesEndRef} />
      </div>

      <form className={styles.inputArea} onSubmit={handleSubmit}>
        <div className={styles.inputWrapper}>
          <textarea
            ref={inputRef}
            className={styles.input}
            placeholder="Ask about your notes..."
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={1}
            disabled={isBusy}
            aria-label="Ask a question"
          />
          {streamStatus === 'streaming' ? (
            <button
              type="button"
              className={styles.stopButton}
              onClick={handleStop}
              aria-label="Stop"
              title="Stop (Esc)"
            >
              <Square size={14} />
            </button>
          ) : (
            <button
              type="submit"
              className={styles.sendButton}
              disabled={!input.trim() || isBusy}
              aria-label="Send"
            >
              <Send size={16} />
            </button>
          )}
        </div>
      </form>
    </div>
  );
}
