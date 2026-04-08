import { useState, useEffect, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { searchFTS, searchSemantic } from '../../api/client';
import type { FTSResult, SemanticResult } from '../../api/types';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { SearchResultSkeleton } from '../../components/Skeleton/Skeleton';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { sanitizeHtml } from '../../lib/sanitize';
import styles from './SearchPage.module.css';

type SearchMode = 'fulltext' | 'semantic';

type SearchResult = {
  note_id: string;
  title: string;
  snippet: string;
  score?: number;
};

export function SearchPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const initialQuery = searchParams.get('q') ?? '';
  const [query, setQuery] = useState(initialQuery);
  const [mode, setMode] = useState<SearchMode>('fulltext');
  const [results, setResults] = useState<SearchResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const abortRef = useRef<AbortController | null>(null);
  const addToast = useToastStore((s) => s.addToast);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Sync query to URL search params. Use a ref for searchParams to avoid
  // re-triggering this effect when setSearchParams updates searchParams.
  const searchParamsRef = useRef(searchParams);
  searchParamsRef.current = searchParams;
  useEffect(() => {
    const currentQ = searchParamsRef.current.get('q') ?? '';
    if (query.trim() && query !== currentQ) {
      setSearchParams({ q: query }, { replace: true });
    } else if (!query.trim() && currentQ) {
      setSearchParams({}, { replace: true });
    }
  }, [query, setSearchParams]);

  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      return;
    }

    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    // Cancel any in-flight search request so stale responses cannot
    // overwrite newer results.
    if (abortRef.current) {
      abortRef.current.abort();
    }

    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      setIsLoading(true);
      try {
        if (mode === 'semantic') {
          const semanticResults = await searchSemantic(query, 20, controller.signal);
          if (controller.signal.aborted) return;
          setResults(
            semanticResults.map((r: SemanticResult) => ({
              note_id: r.note_id,
              title: r.title,
              snippet: r.snippet,
              score: r.score,
            })),
          );
        } else {
          const { results: ftsResults } = await searchFTS(query, 20, 0, controller.signal);
          if (controller.signal.aborted) return;
          setResults(
            ftsResults.map((r: FTSResult) => ({
              note_id: r.note_id,
              title: r.title,
              snippet: r.snippet,
            })),
          );
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return;
        setResults([]);
        const message = err instanceof Error ? err.message : 'Search failed';
        addToast(message, 'error');
      } finally {
        if (!controller.signal.aborted) {
          setIsLoading(false);
        }
      }
    }, 300);

    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
      if (abortRef.current) {
        abortRef.current.abort();
      }
    };
  }, [query, mode, addToast]);

  const handleModeChange = (newMode: SearchMode) => {
    setMode(newMode);
    setResults([]);
  };

  return (
    <div className={styles.page}>
      <div className={styles.searchBar}>
        <input
          ref={inputRef}
          type="text"
          className={styles.searchInput}
          placeholder={
            mode === 'semantic' ? 'Ask a question about your notes...' : 'Search notes...'
          }
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Search notes"
        />
      </div>

      <div className={styles.tabs}>
        <button
          className={`${styles.tab} ${mode === 'fulltext' ? styles.activeTab : ''}`}
          onClick={() => handleModeChange('fulltext')}
        >
          Full-text
        </button>
        <button
          className={`${styles.tab} ${mode === 'semantic' ? styles.activeTab : ''}`}
          onClick={() => handleModeChange('semantic')}
        >
          Semantic
        </button>
      </div>

      <div className={styles.srOnly} aria-live="polite" role="status">
        {!isLoading &&
          query.trim() &&
          (results.length > 0
            ? `${results.length} result${results.length === 1 ? '' : 's'} found`
            : 'No results found')}
      </div>

      {isLoading ? (
        <SearchResultSkeleton count={4} />
      ) : results.length === 0 && query.trim() ? (
        <EmptyState
          heading="No matches"
          subtext={mode === 'semantic' ? 'Try rephrasing your question' : 'Try different keywords'}
        />
      ) : (
        <motion.div
          className={styles.results}
          initial="hidden"
          animate="visible"
          variants={{
            hidden: {},
            visible: { transition: { staggerChildren: 0.03 } },
          }}
        >
          {results.map((result) => (
            <motion.button
              variants={{
                hidden: { opacity: 0, y: 4 },
                visible: { opacity: 1, y: 0 },
              }}
              transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
              key={result.note_id}
              className={styles.resultItem}
              onClick={() => navigate(`/notes/${result.note_id}`)}
            >
              <div className={styles.resultHeader}>
                <h3 className={styles.resultTitle}>{result.title}</h3>
                {result.score !== undefined && (
                  <span className={styles.resultScore}>{Math.round(result.score * 100)}%</span>
                )}
              </div>
              <p
                className={styles.resultSnippet}
                dangerouslySetInnerHTML={{ __html: sanitizeHtml(result.snippet) }}
              />
              {result.score !== undefined && (
                <div className={styles.relevanceBarTrack}>
                  <div
                    className={styles.relevanceBarFill}
                    style={{ width: `${Math.round(result.score * 100)}%` }}
                  />
                </div>
              )}
            </motion.button>
          ))}
        </motion.div>
      )}
    </div>
  );
}
