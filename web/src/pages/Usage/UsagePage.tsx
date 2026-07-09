import { useEffect, useState } from 'react';
import { Gauge } from 'lucide-react';
import { useUsageStore } from '../../stores/usageStore';
import { GenericPageSkeleton } from '../../components/Skeleton/Skeleton';
import type {
  TimeSeriesPoint,
  UsageBudget,
  UsageBudgetUpdate,
  RetrievalSummary,
} from '../../api/types';
import styles from './UsagePage.module.css';

function fmt(n: number): string {
  return n.toLocaleString();
}

function pct(fraction: number): string {
  return `${Math.round(fraction * 100)}%`;
}

export function UsagePage() {
  const summary = useUsageStore((s) => s.summary);
  const byProvider = useUsageStore((s) => s.byProvider);
  const byModel = useUsageStore((s) => s.byModel);
  const byFunction = useUsageStore((s) => s.byFunction);
  const timeseries = useUsageStore((s) => s.timeseries);
  const budget = useUsageStore((s) => s.budget);
  const retrieval = useUsageStore((s) => s.retrieval);
  const isLoading = useUsageStore((s) => s.isLoading);
  const error = useUsageStore((s) => s.error);
  const fetchAll = useUsageStore((s) => s.fetchAll);
  const updateBudget = useUsageStore((s) => s.updateBudget);

  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>
          <Gauge
            size={20}
            style={{ verticalAlign: 'text-bottom', marginRight: 'var(--space-2)' }}
          />
          Usage
        </h1>
        <p className={styles.subtitle}>Token consumption over the last 30 days</p>
      </div>

      {error && <p className={styles.errorMessage}>{error}</p>}

      {isLoading ? (
        <GenericPageSkeleton />
      ) : (
        <>
          <div className={styles.cards}>
            <SummaryCard label="Total tokens" value={fmt(summary?.total_tokens ?? 0)} />
            <SummaryCard label="Billed tokens" value={fmt(summary?.billed_tokens ?? 0)} />
            <SummaryCard label="Local tokens" value={fmt(summary?.local_tokens ?? 0)} />
            <SummaryCard label="API calls" value={fmt(summary?.call_count ?? 0)} />
          </div>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Tokens over time</h2>
            <TokenBars points={timeseries} />
          </section>

          <div className={styles.breakdowns}>
            <Breakdown
              title="By provider"
              rows={byProvider.map((r) => ({ label: r.provider, value: r.total_tokens }))}
            />
            <Breakdown
              title="By model"
              rows={byModel.map((r) => ({ label: r.model, value: r.total_tokens }))}
            />
            <Breakdown
              title="By function"
              rows={byFunction.map((r) => ({ label: r.function, value: r.total_tokens }))}
            />
          </div>

          <BudgetSection
            // Remount (re-seed the form) only when the persisted budget values
            // change, e.g. on initial load and after a save.
            key={budget ? `${budget.enabled}:${budget.period}:${budget.max_tokens}` : 'nobudget'}
            budget={budget}
            onSave={(u) => {
              void updateBudget(u);
            }}
          />

          <RetrievalSection retrieval={retrieval} />
        </>
      )}
    </div>
  );
}

function SummaryCard({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.card}>
      <div className={styles.cardValue}>{value}</div>
      <div className={styles.cardLabel}>{label}</div>
    </div>
  );
}

function Breakdown({ title, rows }: { title: string; rows: { label: string; value: number }[] }) {
  const max = rows.reduce((m, r) => Math.max(m, r.value), 0);
  return (
    <div className={styles.breakdown}>
      <h3 className={styles.breakdownTitle}>{title}</h3>
      {rows.length === 0 ? (
        <p className={styles.empty}>No data</p>
      ) : (
        <ul className={styles.breakdownList}>
          {rows.map((r) => (
            <li key={r.label} className={styles.breakdownRow}>
              <span className={styles.breakdownLabel}>{r.label}</span>
              <span className={styles.breakdownBarTrack}>
                <span
                  className={styles.breakdownBarFill}
                  style={{ width: max > 0 ? `${(r.value / max) * 100}%` : '0%' }}
                />
              </span>
              <span className={styles.breakdownValue}>{fmt(r.value)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// TokenBars renders a hand-rolled SVG bar chart (no chart library) mirroring
// the stat-bar precedent. Bars scale to the max bucket total.
function TokenBars({ points }: { points: TimeSeriesPoint[] }) {
  if (points.length === 0) {
    return <p className={styles.empty}>No usage recorded in this period</p>;
  }
  const width = 640;
  const height = 140;
  const gap = 2;
  const max = points.reduce((m, p) => Math.max(m, p.total_tokens), 0);
  const barWidth = points.length > 0 ? (width - gap * (points.length - 1)) / points.length : 0;

  return (
    <div className={styles.chartWrap}>
      <svg
        className={styles.chart}
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label="Token usage over time"
      >
        {points.map((p, i) => {
          const h = max > 0 ? (p.total_tokens / max) * (height - 4) : 0;
          const x = i * (barWidth + gap);
          const y = height - h;
          return (
            <rect
              key={p.bucket}
              x={x}
              y={y}
              width={barWidth}
              height={h}
              rx={1}
              className={styles.bar}
            >
              <title>{`${p.bucket}: ${fmt(p.total_tokens)} tokens`}</title>
            </rect>
          );
        })}
      </svg>
      <div className={styles.chartAxis}>
        <span>{points[0]?.bucket}</span>
        <span>{points[points.length - 1]?.bucket}</span>
      </div>
    </div>
  );
}

function BudgetSection({
  budget,
  onSave,
}: {
  budget: UsageBudget | null;
  onSave: (u: UsageBudgetUpdate) => void;
}) {
  // Seeded once from props; the parent remounts this component (via key) when
  // the persisted budget changes, so no syncing effect is needed.
  const [enabled, setEnabled] = useState(budget?.enabled ?? false);
  const [period, setPeriod] = useState<'daily' | 'monthly'>(
    budget?.period === 'daily' ? 'daily' : 'monthly',
  );
  const [maxTokens, setMaxTokens] = useState(budget?.max_tokens ?? 0);

  return (
    <section className={styles.section}>
      <h2 className={styles.sectionTitle}>Budget</h2>
      {budget && budget.enabled && (
        <div className={styles.statsBar}>
          <div className={styles.stat}>
            <span className={styles.statValue}>{fmt(budget.used_tokens)}</span> used
          </div>
          <div className={styles.statDivider} />
          <div className={styles.stat}>
            <span className={styles.statValue}>{fmt(budget.remaining_tokens)}</span> remaining
          </div>
          <div className={styles.statDivider} />
          <div className={styles.stat}>
            <span className={styles.statValue}>{fmt(budget.max_tokens)}</span> / {budget.period}
          </div>
        </div>
      )}
      <div className={styles.budgetForm}>
        <div className={styles.field}>
          <label className={styles.fieldLabel} htmlFor="budget-enabled">
            Enforce budget
          </label>
          <select
            id="budget-enabled"
            className={styles.select}
            value={String(enabled)}
            onChange={(e) => setEnabled(e.target.value === 'true')}
          >
            <option value="false">Off</option>
            <option value="true">On</option>
          </select>
        </div>
        <div className={styles.field}>
          <label className={styles.fieldLabel} htmlFor="budget-period">
            Period
          </label>
          <select
            id="budget-period"
            className={styles.select}
            value={period}
            onChange={(e) => setPeriod(e.target.value === 'daily' ? 'daily' : 'monthly')}
          >
            <option value="daily">Daily</option>
            <option value="monthly">Monthly</option>
          </select>
        </div>
        <div className={styles.field}>
          <label className={styles.fieldLabel} htmlFor="budget-max">
            Max tokens
          </label>
          <input
            id="budget-max"
            type="number"
            min={0}
            className={styles.input}
            value={maxTokens}
            onChange={(e) => setMaxTokens(Math.max(0, Number(e.target.value) || 0))}
          />
        </div>
        <button
          className={styles.primaryButton}
          onClick={() => onSave({ enabled, period, max_tokens: maxTokens })}
        >
          Save budget
        </button>
      </div>
    </section>
  );
}

function RetrievalSection({ retrieval }: { retrieval: RetrievalSummary | null }) {
  if (!retrieval) {
    return (
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Retrieval</h2>
        <p className={styles.empty}>Retrieval telemetry is not available</p>
      </section>
    );
  }
  const kinds = retrieval.kinds ?? [];
  return (
    <section className={styles.section}>
      <h2 className={styles.sectionTitle}>Retrieval</h2>
      <div className={styles.statsBar}>
        <div className={styles.stat}>
          <span className={styles.statValue}>{fmt(retrieval.total)}</span> events
        </div>
        <div className={styles.statDivider} />
        <div className={styles.stat}>
          <span className={styles.statValue}>{pct(retrieval.read_after_inject_rate)}</span>{' '}
          read-after-inject
        </div>
        <div className={styles.statDivider} />
        <div className={styles.stat}>
          <span className={styles.statValue}>{fmt(retrieval.injection_events)}</span> injections
        </div>
        <div className={styles.statDivider} />
        <div className={styles.stat}>
          <span className={styles.statValue}>{fmt(retrieval.read_followups)}</span> read followups
        </div>
      </div>
      {kinds.length > 0 && (
        <table className={styles.kindTable}>
          <thead>
            <tr>
              <th>Kind</th>
              <th>Events</th>
              <th>Hits</th>
              <th>Hit rate</th>
            </tr>
          </thead>
          <tbody>
            {kinds.map((k) => (
              <tr key={k.kind}>
                <td>{k.kind}</td>
                <td>{fmt(k.total)}</td>
                <td>{fmt(k.hits)}</td>
                <td>{k.total > 0 ? pct(k.hits / k.total) : '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

export default UsagePage;
