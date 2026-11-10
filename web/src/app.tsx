import { isTelegramLaunch, type Launch } from './platform/launch';

export function App({ launch }: { launch: Launch }) {
  return (
    <main class="placeholder">
      <h1>Incognito Chat</h1>
      <p>Version 2 is being built.</p>
      <p class="muted">{isTelegramLaunch(launch) ? `Opened in Telegram (${launch.platform || 'unknown'})` : 'Opened in a browser'}</p>
    </main>
  );
}
