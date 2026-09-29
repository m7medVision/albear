import '@testing-library/jest-dom';
import * as React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ClientsSection } from '../renderer/sections/ClientsSection';
import { ActivitySection } from '../renderer/sections/ActivitySection';
import { SettingsSection } from '../renderer/sections/SettingsSection';
import { BackupSection } from '../renderer/sections/BackupSection';
import { VaultProvider } from '../renderer/VaultContext';
import { albearMock, ok, fail, UNLOCKED } from './albearMock';

function renderSection(
  node: React.ReactElement,
  overrides: Record<string, unknown> = {},
) {
  const api = albearMock({
    status: jest.fn().mockResolvedValue(UNLOCKED),
    ...overrides,
  });
  window.albear = api;
  render(
    <MemoryRouter>
      <VaultProvider>{node}</VaultProvider>
    </MemoryRouter>,
  );
  return api;
}

const PENDING = {
  pairingId: 'p1',
  kind: 2,
  kindName: 'chrome-extension',
  label: 'Chrome on this machine',
  phrase: 'amber otter kiln',
  capabilities: [
    'vault.status',
    'vault.unlock',
    'records.match',
    'records.createLogin',
  ],
};

describe('pairing approval', () => {
  it('discloses the kind and the exact capabilities approval would grant', async () => {
    renderSection(<ClientsSection />, {
      clientsPending: jest.fn().mockResolvedValue(ok({ pending: [PENDING] })),
    });

    expect(
      await screen.findByText('Chrome on this machine'),
    ).toBeInTheDocument();
    expect(screen.getByText('Chrome extension')).toBeInTheDocument();
    expect(screen.getByText('amber otter kiln')).toBeInTheDocument();
    // The operator consents to the privilege, not to the label: every
    // capability the grant carries has to be on screen before they click.
    for (const cap of PENDING.capabilities) {
      expect(screen.getByText(cap)).toBeInTheDocument();
    }
  });

  it('approves only on an explicit action', async () => {
    const clientsApprove = jest.fn().mockResolvedValue(ok({}));
    renderSection(<ClientsSection />, {
      clientsPending: jest.fn().mockResolvedValue(ok({ pending: [PENDING] })),
      clientsApprove,
    });

    await screen.findByText('Chrome on this machine');
    expect(clientsApprove).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /Approve/ }));
    await waitFor(() => expect(clientsApprove).toHaveBeenCalledWith('p1'));
  });

  it('says so plainly when nothing is pending', async () => {
    renderSection(<ClientsSection />);
    expect(await screen.findByText('No pairing requests')).toBeInTheDocument();
    expect(screen.getByText('No paired clients')).toBeInTheDocument();
  });

  it('does not claim there are no clients when the list failed to load', async () => {
    renderSection(<ClientsSection />, {
      clientsList: jest
        .fn()
        .mockResolvedValue(fail('INTERNAL', 'Internal failure.')),
    });

    expect(
      await screen.findByText('Unable to load clients'),
    ).toBeInTheDocument();
    expect(screen.getByText(/Use Refresh to try again/)).toBeInTheDocument();
    expect(screen.queryByText('No paired clients')).not.toBeInTheDocument();
    expect(screen.queryByText('No pairing requests')).not.toBeInTheDocument();
  });

  it('requires a second action to revoke a paired client', async () => {
    const clientsRevoke = jest.fn().mockResolvedValue(ok({}));
    renderSection(<ClientsSection />, {
      clientsList: jest.fn().mockResolvedValue(
        ok({
          clients: [{ id: 'c1', kind: 2, status: 2, label: 'Chrome' }],
        }),
      ),
      clientsRevoke,
    });

    fireEvent.click(
      await screen.findByRole('button', { name: 'Revoke Chrome' }),
    );
    expect(clientsRevoke).not.toHaveBeenCalled();

    // The safe choice holds focus, so a stray Enter cancels rather than revokes.
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus();
    fireEvent.click(screen.getByRole('button', { name: 'Revoke client' }));
    await waitFor(() => expect(clientsRevoke).toHaveBeenCalledWith('c1'));
  });
});

describe('activity log', () => {
  it('names the event codes and severities rather than showing raw integers', async () => {
    renderSection(<ActivitySection />, {
      events: jest.fn().mockResolvedValue(
        ok({
          events: [
            {
              sequence: 2,
              occurredMs: 1_700_000_000_000,
              severity: 2,
              code: 111,
            },
            {
              sequence: 1,
              occurredMs: 1_699_000_000_000,
              severity: 1,
              code: 101,
            },
          ],
        }),
      ),
    });

    expect(await screen.findByText('unauthorized request')).toBeInTheDocument();
    expect(screen.getByText('vault unlocked')).toBeInTheDocument();
    expect(screen.getByText('warning')).toBeInTheDocument();
  });

  it('renders an unknown code rather than hiding the event', async () => {
    renderSection(<ActivitySection />, {
      events: jest.fn().mockResolvedValue(
        ok({
          events: [{ sequence: 1, occurredMs: 0, severity: 1, code: 999 }],
        }),
      ),
    });
    // A code this build has no name for is still an event that happened.
    expect(await screen.findByText('event 999')).toBeInTheDocument();
  });

  it('shows no record secret in the event view', async () => {
    renderSection(<ActivitySection />, {
      events: jest.fn().mockResolvedValue(
        ok({
          events: [
            {
              sequence: 1,
              occurredMs: 0,
              severity: 1,
              code: 102,
              details: 'idle timeout',
            },
          ],
        }),
      ),
    });

    await screen.findByText('vault locked');
    // The daemon is bound never to put a secret in an event, so this asserts
    // the view adds none of its own: it renders event fields and nothing else.
    expect(document.body.textContent).not.toMatch(
      /password|apiKey|secret value/i,
    );
  });

  it('states an empty log as empty', async () => {
    renderSection(<ActivitySection />);
    expect(
      await screen.findByText('No security events yet'),
    ).toBeInTheDocument();
  });
});

describe('master password change', () => {
  it('labels every field and validates on submit instead of disabling it', async () => {
    const changePassword = jest.fn().mockResolvedValue(ok({}));
    renderSection(<SettingsSection />, { changePassword });

    const current = await screen.findByLabelText('Current master password');
    const next = screen.getByLabelText('New master password');
    const repeat = screen.getByLabelText('Repeat new master password');
    expect(current).toHaveAttribute('autocomplete', 'current-password');
    expect(next).toHaveAttribute('autocomplete', 'new-password');

    const submit = screen.getByRole('button', { name: 'Change password' });
    expect(submit).toBeEnabled();
    fireEvent.click(submit);
    expect(changePassword).not.toHaveBeenCalled();
    expect(current).toHaveAttribute('aria-invalid', 'true');
    expect(current).toHaveAccessibleDescription(
      'Enter your current master password.',
    );
    expect(current).toHaveFocus();

    fireEvent.change(current, { target: { value: 'old' } });
    fireEvent.change(next, { target: { value: 'a new passphrase' } });
    fireEvent.change(repeat, { target: { value: 'a different one' } });
    fireEvent.click(submit);
    expect(changePassword).not.toHaveBeenCalled();
    expect(repeat).toHaveFocus();

    fireEvent.change(repeat, { target: { value: 'a new passphrase' } });
    fireEvent.click(submit);
    await waitFor(() =>
      expect(changePassword).toHaveBeenCalledWith('old', 'a new passphrase'),
    );
  });

  it('puts a wrong current password on the current-password field', async () => {
    renderSection(<SettingsSection />, {
      changePassword: jest.fn().mockResolvedValue(fail('AUTH_FAILED')),
    });

    const current = await screen.findByLabelText('Current master password');
    fireEvent.change(current, { target: { value: 'wrong' } });
    fireEvent.change(screen.getByLabelText('New master password'), {
      target: { value: 'x' },
    });
    fireEvent.change(screen.getByLabelText('Repeat new master password'), {
      target: { value: 'x' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }));

    await waitFor(() =>
      expect(current).toHaveAttribute('aria-invalid', 'true'),
    );
    expect(current).toHaveAccessibleDescription(
      /not your current master password/,
    );
  });
});

describe('backup', () => {
  it('tells the user not to restore a backup that fails to verify, and what to do instead', async () => {
    renderSection(<BackupSection />, {
      backupVerify: jest
        .fn()
        .mockResolvedValue(fail('INTERNAL', 'Internal failure.')),
    });

    fireEvent.click(
      await screen.findByRole('button', { name: /Verify backup/ }),
    );
    expect(
      await screen.findByText('This backup did not verify'),
    ).toBeInTheDocument();
    expect(screen.getByText(/do not restore it/)).toBeInTheDocument();
    expect(screen.getByText(/Choose another backup file/)).toBeInTheDocument();
  });

  it('announces a saved backup politely, with its full path', async () => {
    const path = '/home/me/Documents/albear-backup-1700000000000.abk';
    renderSection(<BackupSection />, {
      backupCreate: jest.fn().mockResolvedValue(ok({ path })),
    });

    fireEvent.click(
      await screen.findByRole('button', { name: /Create backup/ }),
    );
    const status = await screen.findByRole('status');
    expect(status).toHaveTextContent('Backup saved');
    expect(status).toHaveTextContent(path);
  });
});
