import { describe, it, expect } from 'vitest';
import { fuzzyMatch, fuzzyFilter } from './fuzzyMatch';

describe('fuzzyMatch', () => {
  it('returns null for non-matching query', () => {
    expect(fuzzyMatch('xyz', 'hello')).toBeNull();
  });

  it('matches exact substring', () => {
    const result = fuzzyMatch('hel', 'hello');
    expect(result).not.toBeNull();
    expect(result!.matches).toEqual([0, 1, 2]);
  });

  it('matches non-consecutive characters', () => {
    const result = fuzzyMatch('hlo', 'hello');
    expect(result).not.toBeNull();
    // Greedy left-to-right: h=0, l=2 (first 'l'), o=4
    expect(result!.matches).toEqual([0, 2, 4]);
  });

  it('returns empty matches for empty query', () => {
    const result = fuzzyMatch('', 'hello');
    expect(result).not.toBeNull();
    expect(result!.matches).toEqual([]);
    expect(result!.score).toBe(0);
  });

  it('returns null when query is longer than target', () => {
    expect(fuzzyMatch('abcdef', 'abc')).toBeNull();
  });

  it('is case-insensitive', () => {
    const result = fuzzyMatch('HEL', 'hello');
    expect(result).not.toBeNull();
    expect(result!.matches).toEqual([0, 1, 2]);
  });

  it('gives higher score for consecutive matches', () => {
    const consecutive = fuzzyMatch('hel', 'hello');
    const spread = fuzzyMatch('hlo', 'hello');
    expect(consecutive!.score).toBeGreaterThan(spread!.score);
  });

  it('gives bonus for word-start matches', () => {
    const wordStart = fuzzyMatch('n', 'new note');
    expect(wordStart).not.toBeNull();
    // First char gets start-of-string bonus
    expect(wordStart!.score).toBeGreaterThan(0);
  });

  it('gives bonus for string-start matches', () => {
    const startMatch = fuzzyMatch('g', 'Graph view');
    const midMatch = fuzzyMatch('v', 'Graph view');
    // 'g' matches at position 0 (string start bonus +7)
    // 'v' matches at position 6 (word start bonus +5)
    expect(startMatch!.score).toBeGreaterThan(midMatch!.score);
  });

  it('gives case-exact bonus', () => {
    const exact = fuzzyMatch('H', 'Hello');
    const inexact = fuzzyMatch('h', 'Hello');
    expect(exact!.score).toBeGreaterThan(inexact!.score);
  });
});

describe('fuzzyFilter', () => {
  const items = [
    { name: 'Graph view' },
    { name: 'Timeline' },
    { name: 'New note' },
    { name: 'Toggle sidebar' },
  ];

  it('returns all items when query is empty', () => {
    const results = fuzzyFilter('', items, (i) => i.name);
    expect(results).toHaveLength(4);
  });

  it('filters and sorts by score', () => {
    const results = fuzzyFilter('gr', items, (i) => i.name);
    expect(results.length).toBeGreaterThan(0);
    expect(results[0].name).toBe('Graph view');
  });

  it('returns empty array for non-matching query', () => {
    const results = fuzzyFilter('xyz', items, (i) => i.name);
    expect(results).toHaveLength(0);
  });

  it('adds matchScore and matchIndices properties', () => {
    const results = fuzzyFilter('gr', items, (i) => i.name);
    expect(results[0]).toHaveProperty('matchScore');
    expect(results[0]).toHaveProperty('matchIndices');
    expect(results[0].matchScore).toBeGreaterThan(0);
    expect(results[0].matchIndices.length).toBeGreaterThan(0);
  });
});
