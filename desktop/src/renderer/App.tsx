// Desktop vault shell: connection/lock phase, header controls, and the section
// nav that everything else hangs off.
//
// Sections are rendered only in the unlocked phase. That is what implements the
// lock teardown: a lock unmounts them, and React drops their state — revealed
// secrets, open editors, unsaved edits — without anything having to remember to
// clear it.
import * as React from 'react';
import { MemoryRouter, Routes, Route, NavLink, Navigate } from 'react-router-dom';
import {
  Activity,
  Archive,
  KeyRound,
  Lock,
  RefreshCw,
  Settings,
  ShieldAlert,
  Users,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { UpdateBanner } from '@/components/UpdateBanner';
import { UnlockCard } from '@/components/UnlockCard';
import { CreateVaultCard } from '@/components/CreateVaultCard';
import { DaemonSetupCard } from '@/components/DaemonSetupCard';
import { VaultProvider, useVault, type Phase } from '@/VaultContext';
import { RecordsSection } from '@/sections/RecordsSection';
import { ClientsSection } from '@/sections/ClientsSection';
import { ActivitySection } from '@/sections/ActivitySection';
import { BackupSection } from '@/sections/BackupSection';
import { SettingsSection } from '@/sections/SettingsSection';
import { unwrap } from '@/lib/api';
import { cn } from '@/lib/utils';
// The app mark itself, rather than a stand-in glyph. The 128px source keeps it
// crisp on HiDPI at its 28px display size.
import appIcon from '../../assets/icons/128x128.png';
import '@/styles/globals.css';

function badgeFor(phase: Phase): {
  text: string;
  variant: 'default' | 'secondary' | 'destructive' | 'success' | 'outline';
} {
  switch (phase) {
    case 'connecting':
      return { text: 'connecting…', variant: 'outline' };
    case 'service-setup':
      return { text: 'setup needed', variant: 'outline' };
    case 'service-failed':
    case 'service-missing':
    case 'service-unsupported':
    case 'unavailable':
      return { text: 'service unavailable', variant: 'destructive' };
    case 'uninitialized':
      return { text: 'no vault', variant: 'outline' };
    case 'locked':
      return { text: 'locked', variant: 'secondary' };
    // Lock state is the one thing worth spotting without reading: green
    // unlocked, grey locked, red unreachable.
    default:
      return { text: 'unlocked', variant: 'success' };
  }
}

const SECTIONS = [
  { to: '/records', label: 'Records', icon: KeyRound },
  { to: '/clients', label: 'Clients', icon: Users },
  { to: '/activity', label: 'Activity', icon: Activity },
  { to: '/backup', label: 'Backup', icon: Archive },
  { to: '/settings', label: 'Settings', icon: Settings },
];

function SectionNav(): React.ReactElement {
  return (
    <nav className="flex items-center gap-1 border-b border-border">
      {SECTIONS.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          className={({ isActive }) =>
            cn(
              'flex items-center gap-2 px-3 py-2 text-sm rounded-t-md border-b-2 -mb-px transition-colors',
              isActive
                ? 'border-primary text-foreground font-medium'
                : 'border-transparent text-muted-foreground hover:text-foreground',
            )
          }
        >
          <Icon className="size-4" />
          {label}
        </NavLink>
      ))}
    </nav>
  );
}

function HeaderControls(): React.ReactElement | null {
  const { phase, refresh } = useVault();
  if (phase !== 'unlocked') return null;

  async function lock(): Promise<void> {
    try {
      unwrap(await window.albear.lock());
    } finally {
      void refresh();
    }
  }

  // Panic is vault.lock with a different audit code, so a misclick costs no
  // data — it costs a panic event that never happened, in a log an operator is
  // meant to trust. Hence one click and no confirmation (a prompt would defeat
  // the point of a panic control), but sat apart from Lock rather than beside
  // it: two adjacent lookalike buttons is how the misclick happens.
  async function panic(): Promise<void> {
    try {
      unwrap(await window.albear.panic());
    } finally {
      void refresh();
    }
  }

  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        onClick={() => void panic()}
        title="Lock immediately and record a panic event"
        className="text-destructive hover:text-destructive hover:bg-destructive/10"
      >
        <ShieldAlert />
        Panic
      </Button>
      <span aria-hidden className="w-4" />
      <Button variant="secondary" size="sm" onClick={() => void lock()}>
        <Lock />
        Lock
      </Button>
    </>
  );
}

function PhaseScreen(): React.ReactElement | null {
  const { phase, refresh } = useVault();

  if (phase === 'connecting') {
    return (
      <p className="text-sm text-muted-foreground text-center py-10">
        connecting to vaultd…
      </p>
    );
  }

  if (phase === 'service-setup') return <DaemonSetupCard />;

  if (
    phase === 'service-failed' ||
    phase === 'service-missing' ||
    phase === 'service-unsupported' ||
    phase === 'unavailable'
  ) {
    const recovery = {
      'service-failed': {
        title: 'The Albear service did not start',
        detail:
          'Check the user service with systemctl --user status albear-vaultd, then retry.',
      },
      'service-missing': {
        title: 'Albear core is not installed',
        detail:
          'Install the matching albear core package, which provides vaultd and its user service, then retry.',
      },
      'service-unsupported': {
        title: 'Automatic service setup is unavailable',
        detail:
          'This system has no reachable systemd user manager. Start vaultd for your user, then retry.',
      },
      unavailable: {
        title: 'Cannot reach the Albear service',
        detail:
          'The service appears to be running, but its local socket is not ready. Wait a moment and retry.',
      },
    }[phase];

    return (
      <Alert variant="destructive">
        <AlertTitle>{recovery.title}</AlertTitle>
        <AlertDescription className="flex flex-col gap-3">
          <span>{recovery.detail}</span>
          <Button
            variant="secondary"
            size="sm"
            className="self-start"
            onClick={() => void refresh()}
          >
            <RefreshCw />
            Retry
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  if (phase === 'uninitialized') return <CreateVaultCard />;
  if (phase === 'locked') return <UnlockCard />;
  return null;
}

function Shell(): React.ReactElement {
  const { phase } = useVault();
  const badge = badgeFor(phase);

  return (
    <div className="min-h-screen flex flex-col">
      {/* Full-bleed rule, but the controls line up with the content column
          below rather than drifting out to the window edges. */}
      <header className="px-6 py-4 border-b border-border">
        <div className="w-full max-w-3xl mx-auto flex items-center gap-3">
          {/* The mark is drawn for a light field — its own background is
              transparent, so it muddies against a dark header. The white tile
              is fixed in both themes to keep the shield legible. */}
          <img
            src={appIcon}
            alt=""
            className="size-7 rounded-md shrink-0 bg-white p-0.5 ring-1 ring-border"
          />
          {/* No dir="rtl": the flex-1 heading would right-align the wordmark
              away from the logo. Bidi already shapes the word itself. */}
          <h1
            className="flex-1 font-arabic text-2xl font-bold leading-none"
            lang="ar"
          >
            البير
          </h1>
          <HeaderControls />
          <Badge variant={badge.variant}>{badge.text}</Badge>
        </div>
      </header>

      <main className="flex-1 px-6 py-5 w-full max-w-3xl mx-auto flex flex-col gap-4">
        <UpdateBanner />
        {phase === 'unlocked' ? (
          <>
            <SectionNav />
            <Routes>
              <Route path="/records" element={<RecordsSection />} />
              <Route path="/clients" element={<ClientsSection />} />
              <Route path="/activity" element={<ActivitySection />} />
              <Route path="/backup" element={<BackupSection />} />
              <Route path="/settings" element={<SettingsSection />} />
              <Route path="*" element={<Navigate to="/records" replace />} />
            </Routes>
          </>
        ) : (
          <PhaseScreen />
        )}
      </main>
    </div>
  );
}

export default function App(): React.ReactElement {
  // MemoryRouter, not Hash/BrowserRouter: navigation stays in memory and never
  // touches the document URL, so it cannot collide with the top-level
  // navigation blocking the renderer is hardened with, and leaves no history
  // for a compromised renderer to walk.
  return (
    <MemoryRouter initialEntries={['/records']}>
      <VaultProvider>
        <Shell />
      </VaultProvider>
    </MemoryRouter>
  );
}
