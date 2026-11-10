import { t, type Language } from './i18n';
import { isTelegramLaunch, type Launch } from './platform/launch';

export function App({ launch, language }: { launch: Launch; language: Language }) {
  return (
    <main class="placeholder">
      <h1>{t(language, 'appName')}</h1>
      <p>{t(language, 'building')}</p>
      <p class="muted">
        {isTelegramLaunch(launch)
          ? t(language, 'openedInTelegram', { platform: launch.platform || '?' })
          : t(language, 'openedInBrowser')}
      </p>
    </main>
  );
}
