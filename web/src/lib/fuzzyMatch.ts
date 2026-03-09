// fzf-style fuzzy matching for the command palette.
// Characters must appear in order but not consecutively.

export interface FuzzyResult {
  item: string;
  score: number;
  matches: number[]; // indices of matched characters in target
}

// Returns true if the character at pos is at the start of a word
// (after space, dash, underscore, or at string start).
function isWordStart(target: string, pos: number): boolean {
  if (pos === 0) return true;
  const prev = target[pos - 1];
  return prev === ' ' || prev === '-' || prev === '_' || prev === '/';
}

export function fuzzyMatch(query: string, target: string): FuzzyResult | null {
  if (query.length === 0) return { item: target, score: 0, matches: [] };
  if (query.length > target.length) return null;

  const queryLower = query.toLowerCase();
  const targetLower = target.toLowerCase();
  const matches: number[] = [];

  let qi = 0;
  for (let ti = 0; ti < target.length && qi < query.length; ti++) {
    if (targetLower[ti] === queryLower[qi]) {
      matches.push(ti);
      qi++;
    }
  }

  // All query chars must be matched.
  if (qi !== query.length) return null;

  // Scoring
  let score = 0;
  for (let i = 0; i < matches.length; i++) {
    const pos = matches[i];
    // Each matched character: +1
    score += 1;

    // Consecutive matched characters: +3 bonus per consecutive char
    if (i > 0 && matches[i] === matches[i - 1] + 1) {
      score += 3;
    }

    // Match at start of string: +7 bonus
    if (pos === 0) {
      score += 7;
    } else if (isWordStart(target, pos)) {
      // Match at start of word: +5 bonus
      score += 5;
    }

    // Case-exact match: +1 bonus
    if (query[i] === target[pos]) {
      score += 1;
    }
  }

  return { item: target, score, matches };
}

export function fuzzyFilter<T>(
  query: string,
  items: T[],
  getText: (item: T) => string,
): (T & { matchScore: number; matchIndices: number[] })[] {
  const results: (T & { matchScore: number; matchIndices: number[] })[] = [];

  for (const item of items) {
    const text = getText(item);
    const result = fuzzyMatch(query, text);
    if (result) {
      results.push({
        ...item,
        matchScore: result.score,
        matchIndices: result.matches,
      });
    }
  }

  // Sort by score descending
  results.sort((a, b) => b.matchScore - a.matchScore);
  return results;
}
