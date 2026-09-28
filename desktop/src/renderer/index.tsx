import { createRoot } from 'react-dom/client';
import App from './App';
import { EnvironmentBadge } from './components/EnvironmentBadge';

const container = document.getElementById('root') as HTMLElement;
const root = createRoot(container);
root.render(
  <>
    <App />
    <EnvironmentBadge />
  </>,
);
