import { describe, it, expect } from 'vitest';
import { formatDate, formatDateTime } from './dates';

describe('date formatting', () => {
  describe('formatDate', () => {
    it('formats an ISO date string', () => {
      const result = formatDate('2026-03-08T10:30:00Z');
      expect(result).toBe('Mar 8, 2026');
    });

    it('returns the original string for invalid dates', () => {
      const result = formatDate('not-a-date');
      expect(result).toBe('not-a-date');
    });
  });

  describe('formatDateTime', () => {
    it('formats an ISO date-time string', () => {
      const result = formatDateTime('2026-03-08T10:30:00Z');
      // The exact output depends on timezone; check that it contains the date part
      expect(result).toContain('Mar 8, 2026');
    });

    it('returns the original string for invalid dates', () => {
      const result = formatDateTime('bad');
      expect(result).toBe('bad');
    });
  });
});
