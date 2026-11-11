import { render } from 'preact';
import { App } from './app';
import { config } from './config';
import { applyLanguage, pickLanguage } from './i18n';
import { createApi } from './net/api';
import { isTelegramLaunch, parseLaunch } from './platform/launch';
import { connectTelegram } from './platform/telegram';
import { applyTheme } from './platform/theme';
import { createApp } from './state/app';
import { openDatabase, requestPersistence } from './storage/db';
import { databaseName, profileFor } from './storage/profile';
import { openChannel, useProfile } from './sync/locks';
import './app.css';

// Launch data is read once: a reload can lose the URL fragment.
const launch = parseLaunch(location.hash, location.search);
const telegram = connectTelegram(isTelegramLaunch(launch));
const language = pickLanguage(launch.userLanguage, navigator.language);
applyLanguage(language);
applyTheme(launch.themeParams);

if (telegram) {
  telegram.on('theme_changed', (d) => applyTheme(d.theme_params));
  telegram.post('web_app_ready');
  telegram.post('web_app_expand');
  telegram.post('web_app_request_theme');
}

async function main(): Promise<void> {
  const root = document.getElementById('app');
  if (!root) return;
  // Each Telegram account on this app gets its own local profile.
  const profile = await profileFor(launch.userId);
  useProfile(profile);
  const db = await openDatabase(databaseName(profile));
  void requestPersistence();

  let restart = (): void => {};
  const channel = openChannel((event) => {
    // Another copy changed the account (e.g. finished registering): catch up.
    if (event.type === 'account-changed') restart();
  });
  const app = createApp({
    config,
    api: config ? createApi(config.apiUrl) : null,
    db,
    launch,
    bridge: telegram,
    broadcast: (event) => channel?.send(event),
  });
  restart = () => void app.start();
  render(<App app={app} language={language} />, root);
  await app.start();
}

void main();
