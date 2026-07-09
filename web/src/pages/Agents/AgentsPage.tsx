import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Bot } from 'lucide-react';
import { useAgentStore, type SessionStatus } from '../../stores/agentStore';
import { useProjectStore } from '../../stores/projectStore';
import { useAgentWebSocket } from '../../hooks/useWebSocket';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { GenericPageSkeleton } from '../../components/Skeleton/Skeleton';
import { timeAgo } from '../../lib/dates';
import type { AgentMemory } from '../../api/types';
import styles from './AgentsPage.module.css';

type Tab = 'sessions' | 'memories';

const STATUS_FILTERS: { value: SessionStatus; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'completed', label: 'Completed' },
];

// Categories match the backend's agent.MemoryCategories.
const MEMORY_CATEGORIES = [
  'constraint',
  'runbook',
  'protocol',
  'gotcha',
  'decision',
  'refuted',
  'reference',
];

export function AgentsPage() {
  const [tab, setTab] = useState<Tab>('sessions');

  useAgentWebSocket();

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>
          <Bot size={20} style={{ verticalAlign: 'text-bottom', marginRight: 'var(--space-2)' }} />
          Agents
        </h1>
        <p className={styles.subtitle}>Agent working sessions and long-term memory</p>
      </div>

      <div className={styles.tabs}>
        <button
          className={`${styles.tab} ${tab === 'sessions' ? styles.tabActive : ''}`}
          onClick={() => setTab('sessions')}
        >
          Sessions
        </button>
        <button
          className={`${styles.tab} ${tab === 'memories' ? styles.tabActive : ''}`}
          onClick={() => setTab('memories')}
        >
          Memories
        </button>
      </div>

      {tab === 'sessions' ? <SessionsTab /> : <MemoriesTab />}
    </div>
  );
}

function SessionsTab() {
  const sessions = useAgentStore((s) => s.sessions);
  const isLoading = useAgentStore((s) => s.sessionsLoading);
  const error = useAgentStore((s) => s.sessionsError);
  const fetchSessions = useAgentStore((s) => s.fetchSessions);
  const projects = useProjectStore((s) => s.projects);

  const [status, setStatus] = useState<SessionStatus>('all');
  const [project, setProject] = useState('');
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    fetchSessions(status, project);
  }, [status, project, fetchSessions]);

  // Newest-first by creation time.
  const ordered = useMemo(
    () =>
      [...sessions].sort(
        (a, b) => new Date(b.CreatedAt).getTime() - new Date(a.CreatedAt).getTime(),
      ),
    [sessions],
  );

  return (
    <>
      <div className={styles.controls}>
        <div className={styles.toggle}>
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.value}
              className={`${styles.toggleButton} ${status === f.value ? styles.toggleButtonActive : ''}`}
              onClick={() => setStatus(f.value)}
            >
              {f.label}
            </button>
          ))}
        </div>
        <select
          className={styles.select}
          value={project}
          onChange={(e) => setProject(e.target.value)}
          aria-label="Filter sessions by project"
        >
          <option value="">All projects</option>
          {projects.map((p) => (
            <option key={p.id} value={p.slug}>
              {p.name}
            </option>
          ))}
        </select>
      </div>

      {error && <p className={styles.errorMessage}>{error}</p>}

      {isLoading ? (
        <GenericPageSkeleton />
      ) : ordered.length === 0 ? (
        <EmptyState heading="No sessions yet" subtext="Agent working sessions will appear here" />
      ) : (
        <ul className={styles.list}>
          {ordered.map((session) => {
            const isLab = session.Name.startsWith('lab/');
            const open = expanded === session.ID;
            return (
              <li key={session.ID} className={styles.sessionRow}>
                <button
                  className={styles.sessionHeader}
                  onClick={() => setExpanded(open ? null : session.ID)}
                  aria-expanded={open}
                >
                  <span className={styles.sessionName}>{session.Name}</span>
                  {isLab && <span className={styles.labBadge}>lab</span>}
                  <span className={`${styles.statusChip} ${statusClass(session.Status)}`}>
                    {session.Status}
                  </span>
                  {session.ProjectSlug && (
                    <span className={styles.projectTag}>{session.ProjectSlug}</span>
                  )}
                  <span className={styles.age}>{timeAgo(session.CreatedAt)}</span>
                </button>
                {session.Findings && (
                  <p className={`${styles.findings} ${open ? styles.findingsOpen : ''}`}>
                    {session.Findings}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}

function MemoriesTab() {
  const navigate = useNavigate();
  const memories = useAgentStore((s) => s.memories);
  const isLoading = useAgentStore((s) => s.memoriesLoading);
  const error = useAgentStore((s) => s.memoriesError);
  const fetchMemories = useAgentStore((s) => s.fetchMemories);
  const projects = useProjectStore((s) => s.projects);

  const [project, setProject] = useState('');
  const [category, setCategory] = useState('');

  useEffect(() => {
    fetchMemories(project, category);
  }, [project, category, fetchMemories]);

  // Group by category, preserving the canonical category order.
  const grouped = useMemo(() => {
    const map = new Map<string, AgentMemory[]>();
    for (const m of memories) {
      const list = map.get(m.category) ?? [];
      list.push(m);
      map.set(m.category, list);
    }
    const order = [...MEMORY_CATEGORIES, ...map.keys()];
    const seen = new Set<string>();
    const result: { category: string; items: AgentMemory[] }[] = [];
    for (const cat of order) {
      if (seen.has(cat) || !map.has(cat)) continue;
      seen.add(cat);
      result.push({ category: cat, items: map.get(cat)! });
    }
    return result;
  }, [memories]);

  return (
    <>
      <div className={styles.controls}>
        <select
          className={styles.select}
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          aria-label="Filter memories by category"
        >
          <option value="">All categories</option>
          {MEMORY_CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        <select
          className={styles.select}
          value={project}
          onChange={(e) => setProject(e.target.value)}
          aria-label="Filter memories by project"
        >
          <option value="">All projects</option>
          {projects.map((p) => (
            <option key={p.id} value={p.slug}>
              {p.name}
            </option>
          ))}
        </select>
      </div>

      {error && <p className={styles.errorMessage}>{error}</p>}

      {isLoading ? (
        <GenericPageSkeleton />
      ) : grouped.length === 0 ? (
        <EmptyState heading="No memories yet" subtext="Agent long-term memories will appear here" />
      ) : (
        <div className={styles.groups}>
          {grouped.map((group) => (
            <div key={group.category} className={styles.group}>
              <div className={styles.groupHeader}>
                {group.category}
                <span className={styles.groupCount}>{group.items.length}</span>
              </div>
              <ul className={styles.list}>
                {group.items.map((memory) => (
                  <li key={memory.note_id} className={styles.memoryRow}>
                    <button
                      className={styles.memoryButton}
                      onClick={() => navigate(`/notes/${memory.note_id}`)}
                      title="Open memory note"
                    >
                      <span className={styles.memoryTop}>
                        <span className={styles.memoryName}>{memory.name || memory.title}</span>
                        {memory.category === 'refuted' && (
                          <span className={styles.refutedBadge}>REFUTED</span>
                        )}
                        {memory.project && (
                          <span className={styles.projectTag}>{memory.project}</span>
                        )}
                        <span className={styles.age}>{timeAgo(memory.updated_at)}</span>
                      </span>
                      {memory.description && (
                        <span className={styles.memoryDescription}>{memory.description}</span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </>
  );
}

function statusClass(status: string): string {
  if (status === 'active') return styles.statusActive;
  if (status === 'completed') return styles.statusCompleted;
  return styles.statusArchived;
}

export default AgentsPage;
