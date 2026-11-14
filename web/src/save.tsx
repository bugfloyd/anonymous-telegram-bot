// The save page: opened in the device's own browser to save an attachment
// (media/save.ts). It runs without Telegram and touches no stored data.

import { useSignal } from '@preact/signals';
import { useEffect } from 'preact/hooks';
import { config } from './config';
import { locale, t, type Language } from './i18n';
import { fetchForSaving, Gone, type SaveTicket } from './media/save';
import { createApi } from './net/api';
import { xhrTransfer } from './net/transfer';

type State = 'fetching' | 'ready' | 'done' | 'gone' | 'failed' | 'broken';

export function SavePage({ ticket, language }: { ticket: SaveTicket | null; language: Language }) {
  const tr = (key: Parameters<typeof t>[1]) => t(language, key);
  const state = useSignal<State>(ticket && config ? 'fetching' : 'broken');
  const file = useSignal<{ file: File; url: string } | null>(null);
  const progress = useSignal(0);
  const api = config ? createApi(config.apiUrl) : null;

  const load = async () => {
    if (!ticket || !api) return;
    state.value = 'fetching';
    try {
      const f = await fetchForSaving(api, xhrTransfer, ticket, (p) => (progress.value = p));
      file.value = { file: f, url: URL.createObjectURL(f) };
      state.value = 'ready';
    } catch (err) {
      state.value = err instanceof Gone ? 'gone' : 'failed';
    }
  };
  useEffect(() => {
    void load();
  }, []);

  const size = ticket
    ? new Intl.NumberFormat(locale(language), {
        style: 'unit',
        unit: ticket.s < 1024 * 1024 ? 'kilobyte' : 'megabyte',
        unitDisplay: language === 'fa' ? 'long' : 'short',
        maximumFractionDigits: 1,
      }).format(ticket.s < 1024 * 1024 ? Math.max(1, Math.round(ticket.s / 1024)) : ticket.s / 1024 / 1024)
    : '';
  const image = file.value && file.value.file.type.startsWith('image/');
  return (
    <main class="page">
      <h1>{tr('saveTitle')}</h1>
      {ticket && (
        <p class="muted" dir="auto">
          {ticket.n ?? tr('file')} · {size}
        </p>
      )}
      {state.value === 'fetching' && (
        <p role="status">
          {tr('saveFetching')} {Math.round(progress.value * 100)}%
        </p>
      )}
      {state.value === 'ready' && file.value && (
        <>
          {image && <img class="attach-preview" src={file.value.url} alt="" />}
          <a
            class="button primary"
            href={file.value.url}
            download={file.value.file.name}
            onClick={() => {
              setTimeout(() => (state.value = 'done'), 0);
            }}
          >
            {tr('saveButton')}
          </a>
        </>
      )}
      {state.value === 'done' && file.value && (
        <>
          <p class="notice" role="status">
            {tr('saveDone')}
          </p>
          <a class="button secondary" href={file.value.url} download={file.value.file.name}>
            {tr('saveAgain')}
          </a>
        </>
      )}
      {state.value === 'gone' && <p class="notice">{tr('saveGone')}</p>}
      {state.value === 'broken' && <p class="notice">{tr('saveBroken')}</p>}
      {state.value === 'failed' && (
        <>
          <p class="notice" role="alert">
            {tr('errorNetwork')}
          </p>
          <button type="button" class="button secondary" onClick={() => void load()}>
            {tr('tryAgain')}
          </button>
        </>
      )}
    </main>
  );
}
