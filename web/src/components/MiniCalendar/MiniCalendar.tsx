import { useState, useEffect, useMemo, useCallback } from 'react';
import {
  format,
  startOfMonth,
  endOfMonth,
  startOfWeek,
  endOfWeek,
  addMonths,
  subMonths,
  addDays,
  isSameMonth,
  isSameDay,
  isToday,
} from 'date-fns';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { listNotes } from '../../api/client';
import styles from './MiniCalendar.module.css';

interface MiniCalendarProps {
  onDateSelect: (date: string) => void;
}

export function MiniCalendar({ onDateSelect }: MiniCalendarProps) {
  const [currentMonth, setCurrentMonth] = useState(new Date());
  const [dailyDates, setDailyDates] = useState<Set<string>>(new Set());

  // Fetch daily notes for the current month to show dot indicators.
  useEffect(() => {
    const monthStart = startOfMonth(currentMonth);
    const monthEnd = endOfMonth(currentMonth);

    listNotes({
      tag: 'daily',
      since: monthStart.toISOString(),
      until: monthEnd.toISOString(),
      sort: 'created',
      sort_dir: 'desc',
      limit: 31,
    })
      .then(({ notes }) => {
        const dates = new Set<string>();
        for (const note of notes) {
          // Extract date from title (YYYY-MM-DD prefix).
          const match = note.title.match(/^(\d{4}-\d{2}-\d{2})/);
          if (match) {
            dates.add(match[1]);
          }
        }
        setDailyDates(dates);
      })
      .catch(() => {
        setDailyDates(new Set());
      });
  }, [currentMonth]);

  const handlePrevMonth = useCallback(() => {
    setCurrentMonth((prev) => subMonths(prev, 1));
  }, []);

  const handleNextMonth = useCallback(() => {
    setCurrentMonth((prev) => addMonths(prev, 1));
  }, []);

  // Build the calendar grid: start from the Sunday of the week containing
  // the first of the month, end at the Saturday of the week containing the
  // last of the month.
  const weeks = useMemo(() => {
    const monthStart = startOfMonth(currentMonth);
    const monthEnd = endOfMonth(currentMonth);
    const calStart = startOfWeek(monthStart);
    const calEnd = endOfWeek(monthEnd);

    const rows: Date[][] = [];
    let day = calStart;
    let week: Date[] = [];

    while (day <= calEnd) {
      week.push(day);
      if (week.length === 7) {
        rows.push(week);
        week = [];
      }
      day = addDays(day, 1);
    }

    return rows;
  }, [currentMonth]);

  const dayHeaders = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

  return (
    <div className={styles.calendar}>
      <div className={styles.header}>
        <button
          className={styles.navButton}
          onClick={handlePrevMonth}
          aria-label="Previous month"
        >
          <ChevronLeft size={14} />
        </button>
        <span className={styles.monthLabel}>
          {format(currentMonth, 'MMMM yyyy')}
        </span>
        <button
          className={styles.navButton}
          onClick={handleNextMonth}
          aria-label="Next month"
        >
          <ChevronRight size={14} />
        </button>
      </div>

      <div className={styles.grid}>
        {dayHeaders.map((d) => (
          <div key={d} className={styles.dayHeader}>
            {d}
          </div>
        ))}
        {weeks.flat().map((day, i) => {
          const dateStr = format(day, 'yyyy-MM-dd');
          const inMonth = isSameMonth(day, currentMonth);
          const today = isToday(day);
          const hasDaily = dailyDates.has(dateStr);

          return (
            <button
              key={i}
              className={`${styles.dayCell} ${!inMonth ? styles.dayCellOutside : ''} ${today ? styles.dayCellToday : ''}`}
              onClick={() => onDateSelect(dateStr)}
              title={dateStr}
            >
              <span>{format(day, 'd')}</span>
              {hasDaily && <span className={styles.dot} />}
            </button>
          );
        })}
      </div>
    </div>
  );
}
