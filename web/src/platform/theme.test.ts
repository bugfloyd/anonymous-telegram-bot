import { describe, expect, it } from 'vitest';
import { applyTheme } from './theme';

function fakeRoot() {
  const props: Record<string, string> = {};
  const root = { style: { setProperty: (k: string, v: string) => (props[k] = v) }, dataset: {} as Record<string, string> };
  return { root: root as unknown as HTMLElement, props, dataset: root.dataset };
}

describe('applyTheme', () => {
  it('maps Telegram colours onto app tokens', () => {
    const { root, props, dataset } = fakeRoot();
    applyTheme({ bg_color: '#17212b', text_color: '#f5f5f5', button_color: '#5288c1', unknown_color: '#123456' }, root);
    expect(props).toEqual({ '--app-bg': '#17212b', '--app-text': '#f5f5f5', '--app-accent': '#5288c1' });
    expect(dataset.theme).toBe('telegram');
  });

  it('ignores values that are not colours', () => {
    const { root, props, dataset } = fakeRoot();
    applyTheme({ bg_color: 'red; background: url(x)', text_color: 'expression(1)' }, root);
    expect(props).toEqual({});
    expect(dataset.theme).toBeUndefined();
  });
});
