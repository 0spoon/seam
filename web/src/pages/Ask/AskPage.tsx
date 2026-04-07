import { useState, useRef, useEffect, useCallback } from 'react';
import { sanitizeHtml } from '../../lib/sanitize';
import { useNavigate } from 'react-router-dom';
import { Send, Loader2, FileText, Plus, Trash2, ChevronDown } from 'lucide-react';
import {
  createConversation,
  listConversations,
  getConversation,
  addChatMessage,
  deleteConversation,
} from '../../api/client';
import { useWebSocket } from '../../hooks/useWebSocket';
import { send as wsSend, isConnected } from '../../api/ws';
import { renderMarkdown } from '../../lib/markdown';
import { useToastStore } from '../../components/Toast/ToastContainer';
import type { ChatCitation, ChatMessage, Conversation, WSMessage } from '../../api/types';
import styles from './AskPage.module.css';

const STREAM_TIMEOUT_MS = 60_000;
// Maximum number of messages to send to the server for context.
const MAX_HISTORY_MESSAGES = 10;

const STARTER_SUGGESTIONS = [
  'What are my recent notes about?',
  'Find connections between my notes',
  'Summarize my key ideas',
  'What topics do I write about most?',
];

interface DisplayMessage {
  role: 'user' | 'assistant';
  content: string;
  citations?: ChatCitation[];
}

export function AskPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const [input, setInput] = useState('');
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [recentConversations, setRecentConversations] = useState<Conversation[]>([]);
  const [showConvDropdown, setShowConvDropdown] = useState(false);
  const convDropdownRef = useRef<HTMLDivElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const streamingRef = useRef('');
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const isStreamingRef = useRef(false);
  const conversationIdRef = useRef<string | null>(null);
  const [renderedStreaming, setRenderedStreaming] = useState('');
  const renderThrottleRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Keep ref in sync for use in callbacks.
  useEffect(() => {
    conversationIdRef.current = conversationId;
  }, [conversationId]);

  // Throttle markdown rendering of streaming content (~100ms).
  // Always register a cleanup to clear any pending timeout, even when
  // returning early because a throttle is already in progress.
  useEffect(() => {
    if (!streamingContent) {
      setRenderedStreaming('');
      return;
    }
    if (renderThrottleRef.current === undefined) {
      renderThrottleRef.current = setTimeout(() => {
        renderThrottleRef.current = undefined;
        setRenderedStreaming(sanitizeHtml(renderMarkdown(streamingRef.current)));
      }, 100);
    }
    return () => {
      if (renderThrottleRef.current !== undefined) {
        clearTimeout(renderThrottleRef.current);
        renderThrottleRef.current = undefined;
      }
    };
  }, [streamingContent]);

  // Auto-scroll to bottom when messages change.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingContent]);

  // Load most recent conversation on mount and populate conversation list.
  useEffect(() => {
    let cancelled = false;
    async function loadRecentConversation() {
      try {
        const { conversations } = await listConversations(10, 0);
        if (cancelled) return;
        setRecentConversations(conversations);
        if (conversations.length > 0) {
          const recent = conversations[0];
          const { conversation, messages: msgs } = await getConversation(recent.id);
          if (cancelled) return;
          setConversationId(conversation.id);
          setMessages(
            msgs.map((m) => ({
              role: m.role as 'user' | 'assistant',
              content: m.content,
              citations: m.citations,
            })),
          );
        }
      } catch {
        // Silently fail -- show empty state.
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }
    loadRecentConversation();
    return () => { cancelled = true; };
  }, []);

  // Close dropdown on outside click.
  useEffect(() => {
    if (!showConvDropdown) return;
    const handleClick = (e: MouseEvent) => {
      if (convDropdownRef.current && !convDropdownRef.current.contains(e.target as Node)) {
        setShowConvDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [showConvDropdown]);

  const handleSwitchConversation = useCallback(async (convId: string) => {
    setShowConvDropdown(false);
    try {
      const { conversation, messages: msgs } = await getConversation(convId);
      setConversationId(conversation.id);
      setMessages(
        msgs.map((m) => ({
          role: m.role as 'user' | 'assistant',
          content: m.content,
          citations: m.citations,
        })),
      );
    } catch {
      addToast('Failed to load conversation', 'error');
    }
  }, [addToast]);

  // Focus input once loading is complete.
  useEffect(() => {
    if (!isLoading) {
      inputRef.current?.focus();
    }
  }, [isLoading]);

  // Clear any active streaming timeout.
  const clearStreamTimeout = useCallback(() => {
    if (timeoutRef.current !== undefined) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = undefined;
    }
  }, []);

  // Reset streaming timeout. Called when any streaming data arrives.
  const resetStreamTimeout = useCallback(() => {
    clearStreamTimeout();
    timeoutRef.current = setTimeout(() => {
      if (!isStreamingRef.current) return;
      streamingRef.current = '';
      setStreamingContent('');
      setIsStreaming(false);
      isStreamingRef.current = false;
      setMessages((prev) => [
        ...prev,
        {
          role: 'assistant',
          content:
            'Response timed out. The AI service may be unavailable. Please try again.',
        },
      ]);
    }, STREAM_TIMEOUT_MS);
  }, [clearStreamTimeout]);

  // Recover from a streaming error.
  const recoverFromStreamError = useCallback(
    (errorMessage: string) => {
      clearStreamTimeout();
      streamingRef.current = '';
      setStreamingContent('');
      setIsStreaming(false);
      isStreamingRef.current = false;
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: errorMessage },
      ]);
    },
    [clearStreamTimeout],
  );

  // Persist a message to the server (fire-and-forget with error toast).
  const persistMessage = useCallback(
    async (
      convId: string,
      role: string,
      content: string,
      citations?: ChatCitation[],
    ) => {
      try {
        await addChatMessage(convId, { role, content, citations });
      } catch {
        addToast('Failed to save message', 'error');
      }
    },
    [addToast],
  );

  // Handle streaming WebSocket messages.
  const handleWSMessage = useCallback(
    (msg: WSMessage) => {
      if (msg.type === 'chat.stream') {
        const payload = msg.payload as { token: string };
        streamingRef.current += payload.token;
        setStreamingContent((prev) => prev + payload.token);
        resetStreamTimeout();
      } else if (msg.type === 'chat.done') {
        clearStreamTimeout();
        const payload = msg.payload as { citations?: ChatCitation[] };
        const completedContent = streamingRef.current;
        streamingRef.current = '';
        setMessages((prev) => {
          const completed: DisplayMessage = {
            role: 'assistant',
            content: completedContent,
            citations: payload.citations,
          };
          return [...prev, completed];
        });
        setStreamingContent('');
        setIsStreaming(false);
        isStreamingRef.current = false;

        // Persist assistant message.
        const convId = conversationIdRef.current;
        if (convId) {
          persistMessage(convId, 'assistant', completedContent, payload.citations);
        }
      } else if (msg.type === 'chat.error') {
        const payload = msg.payload as { error?: string } | undefined;
        const detail = (payload as { error?: string })?.error ?? 'An error occurred';
        recoverFromStreamError(
          `Failed to get a response: ${detail}. Please try again.`,
        );
      }
    },
    [clearStreamTimeout, resetStreamTimeout, recoverFromStreamError, persistMessage],
  );

  useWebSocket(handleWSMessage);

  // Detect WebSocket disconnection while streaming.
  useEffect(() => {
    if (!isStreaming) return;

    const interval = setInterval(() => {
      if (isStreamingRef.current && !isConnected()) {
        clearInterval(interval);
        recoverFromStreamError(
          'Connection lost during response. Please try again.',
        );
      }
    }, 2000);

    return () => clearInterval(interval);
  }, [isStreaming, recoverFromStreamError]);

  // Clean up timeout on unmount.
  useEffect(() => {
    return () => clearStreamTimeout();
  }, [clearStreamTimeout]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const query = input.trim();
    if (!query || isStreaming) return;

    // Ensure we have a conversation.
    let convId = conversationId;
    if (!convId) {
      try {
        const conv = await createConversation();
        convId = conv.id;
        setConversationId(conv.id);
      } catch {
        addToast('Failed to create conversation', 'error');
        return;
      }
    }

    // Add user message.
    const userMsg: DisplayMessage = { role: 'user', content: query };
    setMessages((prev) => [...prev, userMsg]);
    setInput('');
    setIsStreaming(true);
    isStreamingRef.current = true;

    // Persist user message.
    persistMessage(convId, 'user', query);

    // Build history from previous messages (for multi-turn conversation).
    const allHistory: ChatMessage[] = messages.map((m) => ({
      role: m.role,
      content: m.content,
    }));
    const history = allHistory.slice(-MAX_HISTORY_MESSAGES);

    // Check WebSocket connection before sending to avoid stuck "Thinking..." state.
    if (!isConnected()) {
      addToast('Not connected to server. Please try again.', 'error');
      setIsStreaming(false);
      isStreamingRef.current = false;
      return;
    }

    wsSend('chat.ask', { query, history });
    streamingRef.current = '';
    setStreamingContent('');
    resetStreamTimeout();
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
    setInput('');
    inputRef.current?.focus();
  };

  const handleSuggestionClick = (suggestion: string) => {
    setInput(suggestion);
    // Auto-submit with a slight delay so the input updates visually.
    setTimeout(() => {
      inputRef.current?.form?.requestSubmit();
    }, 0);
  };

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

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerRow}>
          <div>
            <h1 className={styles.title}>Ask Seam</h1>
            <p className={styles.subtitle}>
              Ask questions about your notes. Answers are grounded in your knowledge
              base.
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
        {messages.length === 0 && !isStreaming && (
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

        {messages.map((msg, i) => (
          <div
            key={i}
            className={`${styles.message} ${msg.role === 'user' ? styles.userMessage : styles.assistantMessage}`}
          >
            {msg.role === 'assistant' ? (
              <div
                className={styles.messageContent}
                dangerouslySetInnerHTML={{
                  __html: sanitizeHtml(renderMarkdown(msg.content)),
                }}
              />
            ) : (
              <div className={styles.messageContent}>{msg.content}</div>
            )}
            {msg.citations && msg.citations.length > 0 && (
              <div className={styles.citations}>
                <span className={styles.citationsLabel}>Sources:</span>
                {msg.citations.map((citation) => (
                  <button
                    key={citation.id}
                    className={styles.citationLink}
                    onClick={() => navigate(`/notes/${citation.id}`)}
                    title={citation.title}
                  >
                    <FileText size={12} />
                    <span>{citation.title.length > 30 ? citation.title.slice(0, 30) + '...' : citation.title}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        ))}

        <div aria-live="polite" aria-atomic="false">
          {isStreaming && streamingContent && (
            <div className={`${styles.message} ${styles.assistantMessage}`}>
              <div
                className={styles.messageContent}
                dangerouslySetInnerHTML={{
                  __html: renderedStreaming,
                }}
              />
            </div>
          )}

          {isStreaming && !streamingContent && (
            <div className={`${styles.message} ${styles.assistantMessage}`}>
              <Loader2 size={16} className={styles.spinner} />
              <span className={styles.thinkingText}>Thinking...</span>
            </div>
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
            disabled={isStreaming}
            aria-label="Ask a question"
          />
          <button
            type="submit"
            className={styles.sendButton}
            disabled={!input.trim() || isStreaming}
            aria-label="Send"
          >
            <Send size={16} />
          </button>
        </div>
      </form>
    </div>
  );
}
