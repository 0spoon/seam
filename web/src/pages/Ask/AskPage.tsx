import { useState, useRef, useEffect, useCallback } from 'react';
import { sanitizeHtml } from '../../lib/sanitize';
import { useNavigate } from 'react-router-dom';
import { Send, Loader2, FileText } from 'lucide-react';
import { askSeam } from '../../api/client';
import { useWebSocket } from '../../hooks/useWebSocket';
import { send as wsSend } from '../../api/ws';
import { renderMarkdown } from '../../lib/markdown';
import type { ChatMessage, WSMessage } from '../../api/types';
import styles from './AskPage.module.css';

interface DisplayMessage {
  role: 'user' | 'assistant';
  content: string;
  citations?: string[];
}

export function AskPage() {
  const navigate = useNavigate();
  const [input, setInput] = useState('');
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [useStreaming, setUseStreaming] = useState(true);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const streamingRef = useRef('');

  // Auto-scroll to bottom when messages change.
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingContent]);

  // Focus input on mount.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Handle streaming WebSocket messages.
  const handleWSMessage = useCallback(
    (msg: WSMessage) => {
      if (msg.type === 'chat.stream') {
        const payload = msg.payload as { token: string };
        streamingRef.current += payload.token;
        setStreamingContent((prev) => prev + payload.token);
      } else if (msg.type === 'chat.done') {
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
      }
    },
    [],
  );

  useWebSocket(handleWSMessage);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const query = input.trim();
    if (!query || isStreaming) return;

    // Add user message.
    const userMsg: DisplayMessage = { role: 'user', content: query };
    setMessages((prev) => [...prev, userMsg]);
    setInput('');
    setIsStreaming(true);

    // Build history from previous messages (for multi-turn conversation).
    const history: ChatMessage[] = messages.map((m) => ({
      role: m.role,
      content: m.content,
    }));

    if (useStreaming) {
      // Send via WebSocket for streaming response.
      wsSend('chat.ask', { query, history });
      streamingRef.current = '';
      setStreamingContent('');
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
                  >
                    <FileText size={12} />
                    <span>{noteId.slice(0, 8)}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        ))}

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
