import { useState, useRef, useEffect, useCallback } from 'react';
import { sanitizeHtml } from '../../lib/sanitize';
import { useNavigate } from 'react-router-dom';
import { Send, Loader2, FileText } from 'lucide-react';
import { askSeam, getNote } from '../../api/client';
import { useWebSocket } from '../../hooks/useWebSocket';
import { send as wsSend, isConnected } from '../../api/ws';
import { renderMarkdown } from '../../lib/markdown';
import type { ChatMessage, WSMessage } from '../../api/types';
import styles from './AskPage.module.css';

const STREAM_TIMEOUT_MS = 60_000;
// Maximum number of messages to send to the server for context.
const MAX_HISTORY_MESSAGES = 10;

interface DisplayMessage {
  role: 'user' | 'assistant';
  content: string;
  citations?: string[];
}

// Cache for resolved note titles to avoid repeated fetches.
const noteTitleCache = new Map<string, string>();

async function resolveNoteTitle(noteId: string): Promise<string> {
  if (noteTitleCache.has(noteId)) {
    return noteTitleCache.get(noteId)!;
  }
  try {
    const note = await getNote(noteId);
    noteTitleCache.set(noteId, note.title);
    return note.title;
  } catch {
    return noteId.slice(0, 8);
  }
}

export function AskPage() {
  const navigate = useNavigate();
  const [input, setInput] = useState('');
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [citationTitles, setCitationTitles] = useState<Map<string, string>>(new Map());
  const useStreaming = true;
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const streamingRef = useRef('');
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const isStreamingRef = useRef(false);

  // Auto-scroll to bottom when messages change.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingContent]);

  // Focus input on mount.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Resolve citation note titles when messages with citations appear.
  useEffect(() => {
    const unresolvedIds: string[] = [];
    for (const msg of messages) {
      if (msg.citations) {
        for (const noteId of msg.citations) {
          if (!citationTitles.has(noteId)) {
            unresolvedIds.push(noteId);
          }
        }
      }
    }
    if (unresolvedIds.length === 0) return;

    let cancelled = false;
    Promise.all(
      unresolvedIds.map(async (id) => {
        const title = await resolveNoteTitle(id);
        return [id, title] as const;
      }),
    ).then((results) => {
      if (cancelled) return;
      setCitationTitles((prev) => {
        const next = new Map(prev);
        for (const [id, title] of results) {
          next.set(id, title);
        }
        return next;
      });
    });

    return () => { cancelled = true; };
  }, [messages, citationTitles]);

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

  // Recover from a streaming error. Shared by chat.error handler,
  // timeout, and WS disconnect detection.
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
        const payload = msg.payload as { citations?: string[] };
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
      } else if (msg.type === 'chat.error') {
        const payload = msg.payload as { error?: string } | undefined;
        const detail = (payload as { error?: string })?.error ?? 'An error occurred';
        recoverFromStreamError(
          `Failed to get a response: ${detail}. Please try again.`,
        );
      }
    },
    [clearStreamTimeout, resetStreamTimeout, recoverFromStreamError],
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

    // Add user message.
    const userMsg: DisplayMessage = { role: 'user', content: query };
    setMessages((prev) => [...prev, userMsg]);
    setInput('');
    setIsStreaming(true);
    isStreamingRef.current = true;

    // Build history from previous messages (for multi-turn conversation).
    // Only send the last N messages to keep server context manageable.
    const allHistory: ChatMessage[] = messages.map((m) => ({
      role: m.role,
      content: m.content,
    }));
    const history = allHistory.slice(-MAX_HISTORY_MESSAGES);

    if (useStreaming) {
      // Send via WebSocket for streaming response.
      wsSend('chat.ask', { query, history });
      streamingRef.current = '';
      setStreamingContent('');
      resetStreamTimeout();
    } else {
      // Use HTTP endpoint (non-streaming).
      try {
        const result = await askSeam(query, history);
        setMessages((prev) => [
          ...prev,
          {
            role: 'assistant',
            content: result.response,
            citations: result.citations,
          },
        ]);
      } catch {
        setMessages((prev) => [
          ...prev,
          {
            role: 'assistant',
            content: 'Failed to get a response. Please try again.',
          },
        ]);
      }
      setIsStreaming(false);
      isStreamingRef.current = false;
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit(e);
    }
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Ask Seam</h1>
        <p className={styles.subtitle}>
          Ask questions about your notes. Answers are grounded in your knowledge
          base.
        </p>
      </header>

      <div className={styles.chatArea}>
        {messages.length === 0 && !isStreaming && (
          <div className={styles.emptyState}>
            <p className={styles.emptyText}>
              Ask a question and Seam will search your notes to find the answer.
            </p>
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
                {msg.citations.map((noteId) => (
                  <button
                    key={noteId}
                    className={styles.citationLink}
                    onClick={() => navigate(`/notes/${noteId}`)}
                    title={noteId}
                  >
                    <FileText size={12} />
                    <span>{citationTitles.get(noteId) ?? noteId.slice(0, 8)}</span>
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
                  __html: sanitizeHtml(renderMarkdown(streamingContent)),
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
