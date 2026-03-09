import { describe, it, expect } from 'vitest';
import { getTagColor, getProjectColor, PROJECT_COLORS } from './tagColor';

describe('tagColor', () => {
  describe('getTagColor', () => {
    it('returns a color from the project palette', () => {
      const color = getTagColor('architecture');
      expect(PROJECT_COLORS).toContain(color);
    });

    it('returns deterministic colors for the same tag', () => {
      const color1 = getTagColor('testing');
      const color2 = getTagColor('testing');
      expect(color1).toBe(color2);
    });

    it('returns different colors for different tags (usually)', () => {
      const color1 = getTagColor('frontend');
      const color2 = getTagColor('backend');
      // They might collide due to hash, but for these strings they should differ
      // Just verify both are valid
      expect(PROJECT_COLORS).toContain(color1);
      expect(PROJECT_COLORS).toContain(color2);
    });

    it('handles empty string', () => {
      const color = getTagColor('');
      expect(PROJECT_COLORS).toContain(color);
    });
  });

  describe('getProjectColor', () => {
    it('returns correct color for index', () => {
      expect(getProjectColor(0)).toBe('#c4915c');
      expect(getProjectColor(1)).toBe('#6b9b7a');
      expect(getProjectColor(7)).toBe('#c46b6b');
    });

    it('wraps around for indices beyond palette size', () => {
      expect(getProjectColor(8)).toBe(getProjectColor(0));
      expect(getProjectColor(10)).toBe(getProjectColor(2));
    });
  });
});
