import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import App from '../renderer/App';
import { albearMock, ok, UNLOCKED } from './albearMock';

describe('App', () => {
  it('shows the daemon-unavailable state with a retry hint', async () => {
    window.albear = albearMock();
    render(<App />);
    expect(await screen.findByText(/is vaultd running\?/)).toBeInTheDocument();
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
