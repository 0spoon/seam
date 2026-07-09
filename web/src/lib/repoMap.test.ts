import { describe, it, expect } from 'vitest';
import { parseRepoMap, serializeRepoMap, isAbsolutePath, type RepoMapRow } from './repoMap';

describe('repoMap', () => {
  it('parses a JSON object into rows', () => {
    const rows = parseRepoMap('{"/a/b":"proj-a","/c/d":"proj-c"}');
    expect(rows).toEqual([
      { path: '/a/b', project: 'proj-a' },
      { path: '/c/d', project: 'proj-c' },
    ]);
  });

  it('returns an empty list for empty, invalid, or non-object input', () => {
    expect(parseRepoMap('')).toEqual([]);
    expect(parseRepoMap(undefined)).toEqual([]);
    expect(parseRepoMap('not json')).toEqual([]);
    expect(parseRepoMap('[1,2,3]')).toEqual([]);
    expect(parseRepoMap('"a string"')).toEqual([]);
  });

  it('drops non-string values when parsing', () => {
    expect(parseRepoMap('{"/a":"ok","/b":123}')).toEqual([{ path: '/a', project: 'ok' }]);
  });

  it('serializes rows to a JSON object, dropping blank rows', () => {
    const rows: RepoMapRow[] = [
      { path: '/a/b', project: 'proj-a' },
      { path: '  ', project: 'proj-x' },
      { path: '/c/d', project: '' },
    ];
    expect(serializeRepoMap(rows)).toBe('{"/a/b":"proj-a"}');
  });

  it('round-trips parse -> serialize', () => {
    const raw = '{"/home/user/repo":"my-project","/srv/other":"other"}';
    expect(serializeRepoMap(parseRepoMap(raw))).toBe(raw);
  });

  it('validates absolute paths', () => {
    expect(isAbsolutePath('/usr/local')).toBe(true);
    expect(isAbsolutePath('C:\\Users\\me')).toBe(true);
    expect(isAbsolutePath('D:/repos/x')).toBe(true);
    expect(isAbsolutePath('relative/path')).toBe(false);
    expect(isAbsolutePath('./rel')).toBe(false);
    expect(isAbsolutePath('')).toBe(false);
    expect(isAbsolutePath('   ')).toBe(false);
  });
});
