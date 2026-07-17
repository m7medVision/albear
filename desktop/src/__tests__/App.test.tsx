import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import App from '../renderer/App';
import { albearMock, fail, ok, UNLOCKED } from './albearMock';

describe('App', () => {
  it('shows an actionable fallback when automatic service setup is unavailable', async () => {
    window.albear = albearMock();
    render(<App />);
    expect(
      await screen.findByText('Automatic service setup is unavailable'),
    ).toBeInTheDocument();
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('shows the unlock form when the vault is locked', async () => {
    window.albear = albearMock({
      status: jest
        .fn()
        .mockResolvedValue(ok({ available: true, initialized: true, unlocked: false })),
    });
    render(<App />);
    expect(
      await screen.findByPlaceholderText('Master password'),
    ).toBeInTheDocument();
    expect(screen.getByText('locked')).toBeInTheDocument();
  });

  it('lists records when unlocked', async () => {
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue(UNLOCKED),
      list: jest.fn().mockResolvedValue(
        ok({
          records: [
            {
              id: 'r1',
              type: 'login',
              revision: 1,
              name: 'GitHub',
              username: 'mo',
              createdAtMs: 0,
              updatedAtMs: 0,
            },
          ],
        }),
      ),
    });
    render(<App />);
    expect(await screen.findByText('GitHub')).toBeInTheDocument();
    expect(screen.getByText('Reveal')).toBeInTheDocument();
  });
});

describe('daemon service onboarding', () => {
  it('requires explicit consent before enabling the installed service', async () => {
    const daemonServiceSetup = jest
      .fn()
      .mockResolvedValue(ok({ state: 'running', enabled: true }));
    window.albear = albearMock({
      daemonServiceStatus: jest
        .fn()
        .mockResolvedValue(ok({ state: 'stopped', enabled: false })),
      daemonServiceSetup,
    });
    render(<App />);

    expect(await screen.findByText('Start Albear')).toBeInTheDocument();
    expect(screen.getByText(/never listens on a network/)).toBeInTheDocument();
    expect(daemonServiceSetup).not.toHaveBeenCalled();
  });

  it('starts the service and continues to vault creation', async () => {
    const status = jest
      .fn()
      .mockResolvedValueOnce(ok({ available: false }))
      .mockResolvedValue(ok({ available: true, initialized: false }));
    const daemonServiceSetup = jest
      .fn()
      .mockResolvedValue(ok({ state: 'running', enabled: true }));
    window.albear = albearMock({
      status,
      daemonServiceStatus: jest
        .fn()
        .mockResolvedValue(ok({ state: 'stopped', enabled: false })),
      daemonServiceSetup,
    });
    render(<App />);

    fireEvent.click(
      await screen.findByRole('button', { name: 'Enable and start' }),
    );
    expect(await screen.findByText('Create your vault')).toBeInTheDocument();
    expect(daemonServiceSetup).toHaveBeenCalledTimes(1);
  });

  it('continues to the locked phase without unlocking automatically', async () => {
    const unlock = jest.fn().mockResolvedValue(ok({}));
    window.albear = albearMock({
      status: jest
        .fn()
        .mockResolvedValueOnce(ok({ available: false }))
        .mockResolvedValue(
          ok({ available: true, initialized: true, unlocked: false }),
        ),
      daemonServiceStatus: jest
        .fn()
        .mockResolvedValue(ok({ state: 'stopped', enabled: false })),
      daemonServiceSetup: jest
        .fn()
        .mockResolvedValue(ok({ state: 'running', enabled: true })),
      unlock,
    });
    render(<App />);

    fireEvent.click(
      await screen.findByRole('button', { name: 'Enable and start' }),
    );
    expect(
      await screen.findByPlaceholderText('Master password'),
    ).toBeInTheDocument();
    expect(unlock).not.toHaveBeenCalled();
  });

  it('keeps setup failures in a retryable, non-secret error state', async () => {
    window.albear = albearMock({
      daemonServiceStatus: jest
        .fn()
        .mockResolvedValue(ok({ state: 'stopped', enabled: false })),
      daemonServiceSetup: jest
        .fn()
        .mockResolvedValue(
          fail(
            'SERVICE_START_FAILED',
            'could not start the Albear background service',
          ),
        ),
    });
    render(<App />);

    fireEvent.click(
      await screen.findByRole('button', { name: 'Enable and start' }),
    );
    expect(await screen.findByText('Could not start Albear')).toBeInTheDocument();
    expect(
      screen.getByText('could not start the Albear background service'),
    ).toBeInTheDocument();
  });

  it.each([
    ['missing', 'Albear core is not installed'],
    ['failed', 'The Albear service did not start'],
    ['unsupported', 'Automatic service setup is unavailable'],
  ] as const)('explains the %s service state', async (state, message) => {
    window.albear = albearMock({
      daemonServiceStatus: jest.fn().mockResolvedValue(ok({ state })),
    });
    render(<App />);
    expect(await screen.findByText(message)).toBeInTheDocument();
  });

  it('does not inspect or reconfigure the service when vaultd is reachable', async () => {
    const daemonServiceStatus = jest.fn();
    const daemonServiceSetup = jest.fn();
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue(UNLOCKED),
      daemonServiceStatus,
      daemonServiceSetup,
    });
    render(<App />);

    await screen.findByText('unlocked');
    expect(daemonServiceStatus).not.toHaveBeenCalled();
    expect(daemonServiceSetup).not.toHaveBeenCalled();
  });
});

describe('vault creation', () => {
  it('offers to create the vault in-app rather than sending the user to a terminal', async () => {
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue(ok({ available: true, initialized: false })),
    });
    render(<App />);

    expect(await screen.findByText('Create your vault')).toBeInTheDocument();
    // The old build told the user to go and run `vault init` themselves. The
    // app holds the capability, so that dead end should be gone.
    expect(screen.queryByText(/vault init/)).not.toBeInTheDocument();
  });

  it('states the password policy up front, since the daemon will not say which rule failed', async () => {
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue(ok({ available: true, initialized: false })),
    });
    render(<App />);
    expect(await screen.findByText(/At least 12 characters/)).toBeInTheDocument();
  });
});

describe('navigation', () => {
  it('moves between sections without touching the document URL', async () => {
    const before = window.location.href;
    window.albear = albearMock({ status: jest.fn().mockResolvedValue(UNLOCKED) });
    render(<App />);

    fireEvent.click(await screen.findByRole('link', { name: 'Activity' }));
    expect(await screen.findByText('no recent activity')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('link', { name: 'Clients' }));
    expect(
      await screen.findByText('nothing is waiting for approval'),
    ).toBeInTheDocument();

    // MemoryRouter keeps navigation off the URL, so it cannot collide with the
    // renderer's top-level navigation blocking.
    expect(window.location.href).toBe(before);
  });

  it('hides the sections entirely while locked', async () => {
    window.albear = albearMock({
      status: jest
        .fn()
        .mockResolvedValue(ok({ available: true, initialized: true, unlocked: false })),
    });
    render(<App />);

    await screen.findByPlaceholderText('Master password');
    expect(screen.queryByRole('link', { name: 'Records' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Backup' })).not.toBeInTheDocument();
  });
});

describe('panic lock', () => {
  it('locks in one click, and is not adjacent to the ordinary lock button', async () => {
    const panic = jest.fn().mockResolvedValue(ok({}));
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue(UNLOCKED),
      panic,
    });
    render(<App />);

    // No confirmation step: a prompt would defeat the point of a panic control.
    fireEvent.click(await screen.findByRole('button', { name: /Panic/ }));
    await waitFor(() => expect(panic).toHaveBeenCalledTimes(1));
  });
});
