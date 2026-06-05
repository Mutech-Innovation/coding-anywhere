import '@mantine/core/styles.css';
import '@mantine/notifications/styles.css';
import './styles.css';

import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './App.js';
import { GuestApp } from './GuestApp.js';
import { theme } from './theme.js';

const container = document.getElementById('root');
if (!container) {
  throw new Error('#root not found');
}

// The guest "ask"-share page is a separate, self-contained entry point served
// at /guest. It runs on a guest token and never touches stored credentials, so
// we route it before the main app rather than wiring it into App's shell.
const isGuestRoute = window.location.pathname.replace(/\/+$/, '').endsWith('/guest');

createRoot(container).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme="light">
      <Notifications position="top-right" />
      {isGuestRoute ? <GuestApp /> : <App />}
    </MantineProvider>
  </StrictMode>,
);
