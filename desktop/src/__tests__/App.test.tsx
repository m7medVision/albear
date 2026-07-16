import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import App from '../renderer/App';
import type { AlbearHandler } from '../main/preload';

function albearMock(overrides: Partial<AlbearHandler> = {}): AlbearHandler {
  const empty = { ok: true as const, data: {} };
  return {
    status: jest
      .fn()
      .mockResolvedValue({ ok: true, data: { available: false } }),
    unlock: jest.fn().mockResolvedValue(empty),
    lock: jest.fn().mockResolvedValue(empty),
    list: jest.fn().mockResolvedValue({ ok: true, data: { records: [] } }),
    search: jest.fn().mockResolvedValue({ ok: true, data: { records: [] } }),
    show: jest.fn(),
    reveal: jest.fn(),
    generate: jest.fn(),
    copyText: jest.fn().mockResolvedValue(empty),
    ...overrides,
  } as unknown as AlbearHandler;
}

describe('App', () => {
  it('shows the daemon-unavailable state with a retry hint', async () => {
    window.albear = albearMock();
    render(<App />);
    expect(await screen.findByText(/is vaultd running\?/)).toBeInTheDocument();
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('shows the unlock form when the vault is locked', async () => {
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue({
        ok: true,
        data: { available: true, initialized: true, unlocked: false },
      }),
    });
    render(<App />);
    expect(
      await screen.findByPlaceholderText('Master password'),
    ).toBeInTheDocument();
    expect(screen.getByText('locked')).toBeInTheDocument();
  });

  it('lists records when unlocked', async () => {
    window.albear = albearMock({
      status: jest.fn().mockResolvedValue({
        ok: true,
        data: { available: true, initialized: true, unlocked: true },
      }),
      list: jest.fn().mockResolvedValue({
        ok: true,
        data: {
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
        },
      }),
    });
    render(<App />);
    expect(await screen.findByText('GitHub')).toBeInTheDocument();
    expect(screen.getByText('Reveal')).toBeInTheDocument();
  });
});
