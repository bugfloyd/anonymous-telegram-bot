export const en = {
  appName: 'Incognito Chat',
  languageName: 'English',
  loading: 'Loading…',

  notConfigured: "This build isn't connected to a server.",
  openInTelegram: 'Open Incognito Chat from Telegram to set it up.',

  welcomeTitle: 'Welcome',
  welcomeBody:
    "People can message you here without knowing who you are, and the server can't read what they write. Setting up creates your keys on this device.",
  setUp: 'Set up',
  setupMismatch: 'That setup link was for another setup. Start again here.',
  reopening: 'Telegram is reopening the app to finish setting up…',
  reopenAgain: 'Nothing happened? Tap again',
  registering: 'Setting up…',

  readyTitle: "You're set up",
  readyBody: 'Your account is ready on this device. Links come in the next update.',
  offline: "Can't reach the server right now.",

  replaceTitle: 'You already have an account',
  replaceBody:
    'Your Telegram account is already set up on another device. Setting up here ends that account and all its links, and people will need your new link to reach you.',
  replaceWarning: "If you didn't expect this question, stop: someone may be trying to take over your account.",
  replaceType: 'To continue, type {phrase}',
  replacePhrase: 'start over',
  replaceConfirm: 'End it and set up here',
  cancel: 'Cancel',

  replacedTitle: 'Your account was replaced',
  replacedBody:
    "Your account was replaced by a new setup using your Telegram account. If this wasn't you, your Telegram account was accessed or the app was tampered with.",
  setUpAgain: 'Set up again',

  errorNetwork: "Couldn't reach the server. Check your connection and try again.",
  errorExpired: 'This setup step took too long. Continue to try again.',
  errorRateLimited: 'Too many setups today. Try again tomorrow.',
  errorLoggedOut: 'This device is no longer signed in.',
  errorGeneric: 'Something went wrong. Try again.',
  tryAgain: 'Try again',
  continue: 'Continue',
} as const;

export type Key = keyof typeof en;
