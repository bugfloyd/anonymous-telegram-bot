import { describe, expect, it } from 'vitest';
import { isTelegramLaunch, parseLaunch } from './launch';

const initData = 'auth_date=1700000000&start_param=reg123&user=' + encodeURIComponent('{"id":1,"language_code":"fa"}') + '&signature=abc&hash=def';

describe('parseLaunch', () => {
  it('reads Telegram launch parameters from the fragment', () => {
    const fragment =
      '#tgWebAppData=' + encodeURIComponent(initData) +
      '&tgWebAppVersion=9.6&tgWebAppPlatform=android' +
      '&tgWebAppThemeParams=' + encodeURIComponent('{"bg_color":"#ffffff","text_color":"#000"}');
    const launch = parseLaunch(fragment);
    expect(launch.initData).toBe(initData);
    expect(launch.version).toBe('9.6');
    expect(launch.platform).toBe('android');
    expect(launch.startParam).toBe('reg123');
    expect(launch.themeParams).toEqual({ bg_color: '#ffffff', text_color: '#000' });
    expect(launch.userLanguage).toBe('fa');
    expect(isTelegramLaunch(launch)).toBe(true);
  });

  it('prefers the signed start_param over URL copies', () => {
    const launch = parseLaunch('tgWebAppData=' + encodeURIComponent(initData), '?tgWebAppStartParam=other');
    expect(launch.startParam).toBe('reg123');
  });

  it('falls back to the URL start parameter when initData has none', () => {
    expect(parseLaunch('tgWebAppData=auth_date%3D1', '?tgWebAppStartParam=x').startParam).toBe('x');
  });

  it('handles a page opened outside Telegram', () => {
    const launch = parseLaunch('');
    expect(launch).toEqual({
      initData: '',
      version: '',
      platform: '',
      startParam: '',
      themeParams: {},
      userLanguage: '',
      allowsWriteToPm: false,
      userId: '',
    });
    expect(isTelegramLaunch(launch)).toBe(false);
  });

  it('reads only a well-formed language code from the user', () => {
    const user = (u: string) => parseLaunch('tgWebAppData=' + encodeURIComponent('user=' + encodeURIComponent(u))).userLanguage;
    expect(user('{"id":1,"language_code":"en-GB"}')).toBe('en-GB');
    expect(user('{"id":1,"language_code":"<script>"}')).toBe('');
    expect(user('{"id":1}')).toBe('');
    expect(user('not json')).toBe('');
  });

  it('reads the user ID only when it is a plain positive integer', () => {
    const id = (u: string) => parseLaunch('tgWebAppData=' + encodeURIComponent('user=' + encodeURIComponent(u))).userId;
    expect(id('{"id":123456789,"first_name":"A"}')).toBe('123456789');
    expect(id('{"id":"123"}')).toBe('');
    expect(id('{"id":-5}')).toBe('');
    expect(id('{"id":1.5}')).toBe('');
    expect(id('{}')).toBe('');
  });

  it('reads whether the bot may already message the user', () => {
    const allows = (u: string) => parseLaunch('tgWebAppData=' + encodeURIComponent('user=' + encodeURIComponent(u))).allowsWriteToPm;
    expect(allows('{"id":1,"allows_write_to_pm":true}')).toBe(true);
    expect(allows('{"id":1,"allows_write_to_pm":"true"}')).toBe(false);
    expect(allows('{"id":1}')).toBe(false);
  });

  it('drops malformed or non-colour theme values', () => {
    expect(parseLaunch('tgWebAppThemeParams=not-json').themeParams).toEqual({});
    expect(parseLaunch('tgWebAppThemeParams=' + encodeURIComponent('[1,2]')).themeParams).toEqual({});
    expect(
      parseLaunch('tgWebAppThemeParams=' + encodeURIComponent('{"bg_color":"red","x":"#12","y":5,"z":"#abcdef"}')).themeParams,
    ).toEqual({ z: '#abcdef' });
  });
});
