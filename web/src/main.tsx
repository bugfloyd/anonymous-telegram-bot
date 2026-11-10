import { render } from 'preact';
import { App } from './app';
import { applyLanguage, pickLanguage } from './i18n';
import { isTelegramLaunch, parseLaunch } from './platform/launch';
import { connectTelegram } from './platform/telegram';
import { applyTheme } from './platform/theme';
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

const root = document.getElementById('app');
if (root) render(<App launch={launch} language={language} />, root);
