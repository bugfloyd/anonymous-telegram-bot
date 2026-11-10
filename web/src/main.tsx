import { render } from 'preact';
import { App } from './app';
import { parseLaunch } from './platform/launch';
import './app.css';

const launch = parseLaunch(location.hash, location.search);
const root = document.getElementById('app');
if (root) render(<App launch={launch} />, root);
