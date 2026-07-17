import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ClientsSection } from '../renderer/sections/ClientsSection';
import { ActivitySection } from '../renderer/sections/ActivitySection';
import { VaultProvider } from '../renderer/VaultContext';
import { albearMock, ok, UNLOCKED } from './albearMock';

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

    expect(await screen.findByText('Chrome on this machine')).toBeInTheDocument();
    expect(screen.getByText('chrome-extension')).toBeInTheDocument();
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
    expect(
      await screen.findByText('nothing is waiting for approval'),
    ).toBeInTheDocument();
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

    fireEvent.click(await screen.findByRole('button', { name: 'Revoke Chrome' }));
    expect(clientsRevoke).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await waitFor(() => expect(clientsRevoke).toHaveBeenCalledWith('c1'));
  });
});

describe('activity log', () => {
  it('names the event codes and severities rather than showing raw integers', async () => {
    renderSection(<ActivitySection />, {
      events: jest.fn().mockResolvedValue(
        ok({
          events: [
            { sequence: 2, occurredMs: 1_700_000_000_000, severity: 2, code: 111 },
            { sequence: 1, occurredMs: 1_699_000_000_000, severity: 1, code: 101 },
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
            { sequence: 1, occurredMs: 0, severity: 1, code: 102, details: 'idle timeout' },
          ],
        }),
      ),
    });

    await screen.findByText('vault locked');
    // The daemon is bound never to put a secret in an event, so this asserts
    // the view adds none of its own: it renders event fields and nothing else.
    expect(document.body.textContent).not.toMatch(/password|apiKey|secret value/i);
  });

  it('states an empty log as empty', async () => {
    renderSection(<ActivitySection />);
    expect(await screen.findByText('no recent activity')).toBeInTheDocument();
  });
});
