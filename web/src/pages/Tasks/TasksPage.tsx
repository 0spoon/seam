import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ListChecks, Square, CheckSquare } from 'lucide-react';
import { useTaskStore } from '../../stores/taskStore';
import { useProjectStore } from '../../stores/projectStore';
import { EmptyState } from '../../components/EmptyState/EmptyState';
import { GenericPageSkeleton } from '../../components/Skeleton/Skeleton';
import styles from './TasksPage.module.css';

type DoneFilter = 'open' | 'done' | 'all';

const DONE_FILTERS: { value: DoneFilter; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'done', label: 'Done' },
  { value: 'all', label: 'All' },
];

function doneParam(filter: DoneFilter): boolean | undefined {
  if (filter === 'open') return false;
  if (filter === 'done') return true;
  return undefined;
}

export function TasksPage() {
  const navigate = useNavigate();
  const tasks = useTaskStore((s) => s.tasks);
  const summary = useTaskStore((s) => s.summary);
  const isLoading = useTaskStore((s) => s.isLoading);
  const error = useTaskStore((s) => s.error);
  const fetchTasks = useTaskStore((s) => s.fetchTasks);
  const fetchSummary = useTaskStore((s) => s.fetchSummary);
  const toggleTask = useTaskStore((s) => s.toggleTask);
  const projects = useProjectStore((s) => s.projects);

  const [doneFilter, setDoneFilter] = useState<DoneFilter>('open');
  const [projectId, setProjectId] = useState('');

  useEffect(() => {
    fetchTasks({ done: doneParam(doneFilter), projectId: projectId || undefined });
    fetchSummary(projectId || undefined);
  }, [doneFilter, projectId, fetchTasks, fetchSummary]);

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>
          <ListChecks
            size={20}
            style={{ verticalAlign: 'text-bottom', marginRight: 'var(--space-2)' }}
          />
          Tasks
        </h1>
        <p className={styles.subtitle}>Checkbox items extracted from your notes</p>
      </div>

      <div className={styles.controls}>
        <div className={styles.toggle}>
          {DONE_FILTERS.map((f) => (
            <button
              key={f.value}
              className={`${styles.toggleButton} ${doneFilter === f.value ? styles.toggleButtonActive : ''}`}
              onClick={() => setDoneFilter(f.value)}
            >
              {f.label}
            </button>
          ))}
        </div>
        <select
          className={styles.select}
          value={projectId}
          onChange={(e) => setProjectId(e.target.value)}
          aria-label="Filter by project"
        >
          <option value="">All projects</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </div>

      <div className={styles.statsBar}>
        <div className={styles.stat}>
          <span className={styles.statValue}>{summary.open}</span> open
        </div>
        <div className={styles.statDivider} />
        <div className={styles.stat}>
          <span className={styles.statValue}>{summary.done}</span> done
        </div>
        <div className={styles.statDivider} />
        <div className={styles.stat}>
          <span className={styles.statValue}>{summary.total}</span> total
        </div>
      </div>

      {error && <p className={styles.errorMessage}>{error}</p>}

      {isLoading ? (
        <GenericPageSkeleton />
      ) : tasks.length === 0 ? (
        <EmptyState
          heading="No tasks here"
          subtext="Add checkbox items to your notes and they will appear here"
        />
      ) : (
        <ul className={styles.list}>
          {tasks.map((task) => (
            <li key={task.id} className={styles.row}>
              <button
                className={styles.checkbox}
                onClick={() => toggleTask(task.id, !task.done)}
                aria-label={task.done ? 'Mark as open' : 'Mark as done'}
                aria-pressed={task.done}
              >
                {task.done ? <CheckSquare size={16} /> : <Square size={16} />}
              </button>
              <button
                className={`${styles.content} ${task.done ? styles.contentDone : ''}`}
                onClick={() => navigate(`/notes/${task.note_id}`)}
                title="Open source note"
              >
                {task.content || '(empty)'}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default TasksPage;
