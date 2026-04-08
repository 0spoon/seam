import { Link } from 'react-router-dom';
import {
  FilePlus,
  FileEdit,
  FolderPlus,
  Square,
  CheckSquare,
  ExternalLink,
  Network,
  BookMarked,
} from 'lucide-react';
import { sanitizeHtml } from '../../lib/sanitize';
import styles from './renderers.module.css';

// A toolRenderer receives the parsed JSON result and returns JSX, or null
// when the shape is not what it expects (so the caller can fall back to
// genericJsonRenderer). These are plain render functions, not React
// components, so they can be looked up at runtime via the registry without
// tripping the "no components created in render" rule. Names are camelCase
// to keep the lint rule from misclassifying them as components.
type ToolRenderer = (result: unknown) => React.ReactNode;

// --- shape guards ------------------------------------------------------

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function asString(v: unknown): string | undefined {
  return typeof v === 'string' ? v : undefined;
}

function asNumber(v: unknown): number | undefined {
  return typeof v === 'number' ? v : undefined;
}

function asArray(v: unknown): unknown[] | undefined {
  return Array.isArray(v) ? v : undefined;
}

function asStringArray(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined;
  return v.filter((x): x is string => typeof x === 'string');
}

function truncate(text: string, n: number): string {
  if (text.length <= n) return text;
  return text.slice(0, n) + '...';
}

// --- generic fallback --------------------------------------------------

const genericJsonRenderer: ToolRenderer = (result) => {
  const text = typeof result === 'string' ? result : JSON.stringify(result, null, 2);
  return <pre className={styles.jsonBlock}>{text}</pre>;
};

// --- per-tool renderers ------------------------------------------------

const searchNotesRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const results = asArray(result.results);
  if (!results) return null;
  const total = asNumber(result.total) ?? results.length;
  return (
    <div>
      <p className={styles.summary}>
        {total} match{total === 1 ? '' : 'es'}
      </p>
      <ol className={styles.list}>
        {results.map((item, i) => {
          if (!isObject(item)) return null;
          const noteId = asString(item.note_id) ?? asString(item.id) ?? '';
          const title = asString(item.title) ?? 'Untitled';
          const snippet = asString(item.snippet) ?? '';
          return (
            <li key={`${noteId}-${i}`} className={styles.row}>
              <div>
                <span className={styles.index}>{i + 1}.</span>
                {noteId ? (
                  <Link to={`/notes/${noteId}`} className={styles.rowLink}>
                    {title}
                  </Link>
                ) : (
                  <span>{title}</span>
                )}
              </div>
              {snippet && (
                <div
                  className={styles.snippet}
                  dangerouslySetInnerHTML={{ __html: sanitizeHtml(snippet) }}
                />
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
};

const readNoteRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const title = asString(result.title);
  if (!id || !title) return null;
  const body = asString(result.body) ?? '';
  const tags = asStringArray(result.tags) ?? [];
  return (
    <div>
      <h4 className={styles.title}>{title}</h4>
      {tags.length > 0 && (
        <div className={styles.tags}>
          {tags.map((t) => (
            <span key={t} className={styles.tag}>
              #{t}
            </span>
          ))}
        </div>
      )}
      {body && <pre className={styles.bodySnippet}>{truncate(body, 200)}</pre>}
      <Link to={`/notes/${id}`} className={styles.openButton}>
        <ExternalLink size={12} /> Open
      </Link>
    </div>
  );
};

const listNotesRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const notes = asArray(result.notes);
  if (!notes) return null;
  const total = asNumber(result.total) ?? notes.length;
  return (
    <div>
      <p className={styles.summary}>
        {total} note{total === 1 ? '' : 's'}
      </p>
      <ol className={styles.list}>
        {notes.map((item, i) => {
          if (!isObject(item)) return null;
          const noteId = asString(item.id) ?? '';
          const title = asString(item.title) ?? 'Untitled';
          const tags = asStringArray(item.tags) ?? [];
          return (
            <li key={`${noteId}-${i}`} className={styles.row}>
              <div>
                <span className={styles.index}>{i + 1}.</span>
                {noteId ? (
                  <Link to={`/notes/${noteId}`} className={styles.rowLink}>
                    {title}
                  </Link>
                ) : (
                  <span>{title}</span>
                )}
              </div>
              {tags.length > 0 && (
                <div className={styles.tags}>
                  {tags.slice(0, 6).map((t) => (
                    <span key={t} className={styles.tag}>
                      #{t}
                    </span>
                  ))}
                </div>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
};

const createNoteRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const title = asString(result.title) ?? 'Untitled';
  if (!id) return null;
  return (
    <div className={styles.rowInline}>
      <FilePlus size={14} className={styles.rowIcon} />
      <span>Created note</span>
      <Link to={`/notes/${id}`} className={styles.rowLink}>
        {title}
      </Link>
    </div>
  );
};

const updateNoteRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const title = asString(result.title) ?? 'Untitled';
  if (!id) return null;
  return (
    <div className={styles.rowInline}>
      <FileEdit size={14} className={styles.rowIcon} />
      <span>Updated note</span>
      <Link to={`/notes/${id}`} className={styles.rowLink}>
        {title}
      </Link>
    </div>
  );
};

const appendNoteRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const title = asString(result.title) ?? 'Untitled';
  if (!id) return null;
  return (
    <div className={styles.rowInline}>
      <FilePlus size={14} className={styles.rowIcon} />
      <span>Appended to</span>
      <Link to={`/notes/${id}`} className={styles.rowLink}>
        {title}
      </Link>
    </div>
  );
};

const listProjectsRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const projects = asArray(result.projects);
  if (!projects) return null;
  return (
    <div className={styles.chipRow}>
      {projects.map((p, i) => {
        if (!isObject(p)) return null;
        const pid = asString(p.id) ?? '';
        const name = asString(p.name) ?? 'Untitled';
        if (!pid) {
          return (
            <span key={i} className={styles.chip}>
              {name}
            </span>
          );
        }
        return (
          <Link key={pid} to={`/projects/${pid}`} className={styles.chip}>
            {name}
          </Link>
        );
      })}
    </div>
  );
};

const createProjectRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const name = asString(result.name) ?? 'Untitled';
  if (!id) return null;
  return (
    <div className={styles.rowInline}>
      <FolderPlus size={14} className={styles.rowIcon} />
      <span>Created project</span>
      <Link to={`/projects/${id}`} className={styles.rowLink}>
        {name}
      </Link>
    </div>
  );
};

const listTasksRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const tasks = asArray(result.tasks);
  if (!tasks) return null;
  const total = asNumber(result.total) ?? tasks.length;
  return (
    <div>
      <p className={styles.summary}>
        {total} task{total === 1 ? '' : 's'}
      </p>
      <ul className={styles.list}>
        {tasks.map((t, i) => {
          if (!isObject(t)) return null;
          const done = t.done === true;
          const content = asString(t.content) ?? asString(t.text) ?? asString(t.title) ?? '';
          return (
            <li key={i} className={`${styles.taskRow} ${done ? styles.taskDone : ''}`}>
              {done ? (
                <CheckSquare size={14} className={styles.taskIcon} />
              ) : (
                <Square size={14} className={styles.taskIcon} />
              )}
              <span>{content}</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
};

const toggleTaskRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const done = result.done === true;
  return (
    <div className={styles.rowInline}>
      {done ? (
        <CheckSquare size={14} className={styles.rowIcon} />
      ) : (
        <Square size={14} className={styles.rowIcon} />
      )}
      <span>Marked task as {done ? 'done' : 'pending'}</span>
    </div>
  );
};

const getDailyNoteRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const id = asString(result.id);
  const title = asString(result.title);
  if (!id || !title) return null;
  const body = asString(result.body) ?? '';
  return (
    <div>
      <div className={styles.headerRow}>
        <h4 className={styles.title}>{title}</h4>
        <span className={styles.badge}>Today</span>
      </div>
      {body && <pre className={styles.bodySnippet}>{truncate(body, 200)}</pre>}
      <Link to={`/notes/${id}`} className={styles.openButton}>
        <ExternalLink size={12} /> Open
      </Link>
    </div>
  );
};

const getGraphRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const nodes = asArray(result.nodes) ?? [];
  const edges = asArray(result.edges) ?? [];
  return (
    <div className={styles.rowInline}>
      <Network size={14} className={styles.rowIcon} />
      <span>
        {nodes.length} node{nodes.length === 1 ? '' : 's'}, {edges.length} edge
        {edges.length === 1 ? '' : 's'}
      </span>
      <Link to="/graph" className={styles.rowLink}>
        View graph
      </Link>
    </div>
  );
};

const findRelatedRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const results = asArray(result.results);
  if (!results) return null;
  return (
    <ol className={styles.list}>
      {results.map((item, i) => {
        if (!isObject(item)) return null;
        const noteId = asString(item.note_id) ?? asString(item.id) ?? '';
        const title = asString(item.title) ?? 'Untitled';
        const score = asNumber(item.score);
        return (
          <li key={`${noteId}-${i}`} className={styles.row}>
            <div>
              <span className={styles.index}>{i + 1}.</span>
              {noteId ? (
                <Link to={`/notes/${noteId}`} className={styles.rowLink}>
                  {title}
                </Link>
              ) : (
                <span>{title}</span>
              )}
              {score !== undefined && <span className={styles.badge}>{score.toFixed(2)}</span>}
            </div>
          </li>
        );
      })}
    </ol>
  );
};

const getCurrentTimeRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const datetime = asString(result.datetime);
  if (!datetime) return null;
  return <span className={styles.inlinePill}>{datetime}</span>;
};

const searchConversationsRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const messages = asArray(result.messages);
  if (!messages) return null;
  return (
    <ul className={styles.list}>
      {messages.map((m, i) => {
        if (!isObject(m)) return null;
        const role = asString(m.role) ?? 'unknown';
        const content = asString(m.content) ?? '';
        return (
          <li key={i} className={styles.row}>
            <div className={styles.headerRow}>
              <span className={styles.role}>{role}</span>
            </div>
            <div className={styles.snippet}>{truncate(content, 200)}</div>
          </li>
        );
      })}
    </ul>
  );
};

const saveMemoryRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const category = asString(result.category) ?? 'unknown';
  return (
    <div className={styles.rowInline}>
      <BookMarked size={14} className={styles.rowIcon} />
      <span>Saved memory in {category}</span>
    </div>
  );
};

const searchMemoriesRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const memories = asArray(result.memories);
  if (!memories) return null;
  return (
    <ul className={styles.list}>
      {memories.map((m, i) => {
        if (!isObject(m)) return null;
        const content = asString(m.content) ?? '';
        const category = asString(m.category) ?? '';
        return (
          <li key={i} className={styles.memoryCard}>
            <div className={styles.memoryContent}>{content}</div>
            {category && <span className={styles.badge}>{category}</span>}
          </li>
        );
      })}
    </ul>
  );
};

const getProfileRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  const entries = Object.entries(result).filter(
    ([, v]) => v !== null && v !== undefined && v !== '',
  );
  if (entries.length === 0) return null;
  return (
    <table className={styles.kvTable}>
      <tbody>
        {entries.map(([k, v]) => (
          <tr key={k} className={styles.kvRow}>
            <th scope="row" className={styles.kvKey}>
              {k}
            </th>
            <td className={styles.kvValue}>
              {typeof v === 'string' || typeof v === 'number' ? String(v) : JSON.stringify(v)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
};

const updateProfileRenderer: ToolRenderer = (result) => {
  if (!isObject(result)) return null;
  if (result.updated !== true) return null;
  return (
    <div className={styles.rowInline}>
      <FileEdit size={14} className={styles.rowIcon} />
      <span>Profile updated</span>
    </div>
  );
};

// --- registry ----------------------------------------------------------

const REGISTRY: Record<string, ToolRenderer> = {
  search_notes: searchNotesRenderer,
  read_note: readNoteRenderer,
  list_notes: listNotesRenderer,
  create_note: createNoteRenderer,
  update_note: updateNoteRenderer,
  append_to_note: appendNoteRenderer,
  list_projects: listProjectsRenderer,
  create_project: createProjectRenderer,
  list_tasks: listTasksRenderer,
  toggle_task: toggleTaskRenderer,
  get_daily_note: getDailyNoteRenderer,
  get_graph: getGraphRenderer,
  find_related: findRelatedRenderer,
  get_current_time: getCurrentTimeRenderer,
  search_conversations: searchConversationsRenderer,
  save_memory: saveMemoryRenderer,
  search_memories: searchMemoriesRenderer,
  get_profile: getProfileRenderer,
  update_profile: updateProfileRenderer,
};

// renderToolResult looks up a per-tool renderer, calls it as a plain
// function (not a JSX element), and falls back to the generic JSON pretty
// printer when the registry has no entry or the entry returned null
// because the result didn't match its expected shape.
export function renderToolResult(toolName: string, result: unknown): React.ReactNode {
  const renderer = REGISTRY[toolName];
  if (renderer) {
    const out = renderer(result);
    if (out !== null && out !== undefined) {
      return out;
    }
  }
  return genericJsonRenderer(result);
}
