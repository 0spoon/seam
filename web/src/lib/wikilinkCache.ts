import type { ResolvedLink } from '../api/types';

const MAX_CACHE = 50;
const cache = new Map<string, ResolvedLink>();

export function getCached(title: string): ResolvedLink | undefined {
  return cache.get(title.toLowerCase());
}

export function setCache(title: string, result: ResolvedLink): void {
  const key = title.toLowerCase();
  if (cache.size >= MAX_CACHE) {
    // Remove oldest entry (first in Map iteration order).
    const firstKey = cache.keys().next().value;
    if (firstKey !== undefined) cache.delete(firstKey);
  }
  cache.set(key, result);
}

export function invalidateCache(): void {
  cache.clear();
}
