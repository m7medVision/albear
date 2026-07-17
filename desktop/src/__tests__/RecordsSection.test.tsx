import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { RecordsSection } from '../renderer/sections/RecordsSection';
import { VaultProvider } from '../renderer/VaultContext';
import { albearMock, ok, fail, UNLOCKED } from './albearMock';
import type { RecordView, SecretView } from '../shared/vaultTypes';

const LOGIN: RecordView = {
  id: 'r1',
  type: 'login',
  revision: 4,
  name: 'GitHub',
  username: 'mo',
  urls: ['https://github.com'],
  urlEntries: [{ url: 'https://github.com', sub: true }],
  tags: ['work'],
  createdAtMs: 0,
  updatedAtMs: 0,
};

const SECRET: SecretView = {
  password: 'correct-horse-battery-staple',
  notes: 'recovery codes in the safe',
  custom: { pin: '1234' },
};

function renderSection(overrides: Record<string, unknown> = {}) {
  const api = albearMock({
    status: jest.fn().mockResolvedValue(UNLOCKED),
    list: jest.fn().mockResolvedValue(ok({ records: [LOGIN] })),
    reveal: jest.fn().mockResolvedValue(ok(SECRET)),
    ...overrides,
  });
  window.albear = api;
  render(
    <MemoryRouter>
      <VaultProvider>
        <RecordsSection />
      </VaultProvider>
    </MemoryRouter>,
  );
  return api;
}

/** Open the editor on the seeded record and wait for it to populate. */
async function openEditor() {
  fireEvent.click(await screen.findByRole('button', { name: 'Edit GitHub' }));
  await screen.findByText('Edit record');
}

function payloadOf(mock: unknown): Record<string, unknown> {
  const calls = (mock as jest.Mock).mock.calls;
  return calls[0][0] as Record<string, unknown>;
}

describe('editing an existing record', () => {
  it('reveals the record before opening, so the form holds its secrets', async () => {
    const api = renderSection();
    await openEditor();
    // Without this the form would open empty and saving would write empty
    // secrets over the real ones: update replaces, it does not patch.
    expect(api.reveal).toHaveBeenCalledWith('r1');
    expect(screen.getByDisplayValue(SECRET.password!)).toBeInTheDocument();
  });

  it('keeps every secret when only the name is edited', async () => {
    const update = jest.fn().mockResolvedValue(ok({}));
    renderSection({ update });
    await openEditor();

    fireEvent.change(screen.getByDisplayValue('GitHub'), {
      target: { value: 'GitHub (work)' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    const payload = payloadOf(update);
    expect(payload.name).toBe('GitHub (work)');
    // The whole point: an untouched secret must survive the round trip.
    expect(payload.password).toBe(SECRET.password);
    expect(payload.notes).toBe(SECRET.notes);
    expect(payload.custom).toEqual({ pin: '1234' });
  });

  it('keeps the subdomain opt-in when an unrelated field is edited', async () => {
    const update = jest.fn().mockResolvedValue(ok({}));
    renderSection({ update });
    await openEditor();

    fireEvent.change(screen.getByDisplayValue('mo'), {
      target: { value: 'mohammed' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    const payload = payloadOf(update);
    // Sending `urls` instead would make the daemon default this back to exact.
    expect(payload.urlEntries).toEqual([
      { url: 'https://github.com', sub: true },
    ]);
    expect(payload).not.toHaveProperty('urls');
  });

  it('sends the revision it revealed at, so the daemon can spot a conflict', async () => {
    const update = jest.fn().mockResolvedValue(ok({}));
    renderSection({ update });
    await openEditor();
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(payloadOf(update).expectedRevision).toBe(4);
    expect(payloadOf(update).id).toBe('r1');
  });

  it('lets the user clear a secret deliberately', async () => {
    const update = jest.fn().mockResolvedValue(ok({}));
    renderSection({ update });
    await openEditor();

    fireEvent.change(screen.getByDisplayValue(SECRET.password!), {
      target: { value: '' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    // An emptied field is a deliberate clear, and must reach the daemon as one
    // — which is exactly why an untouched field cannot be left off the payload.
    expect(payloadOf(update).password).toBe('');
  });

  it('does not offer to change the type, which the daemon would ignore', async () => {
    renderSection();
    await openEditor();
    expect(screen.getByLabelText('Type')).toBeDisabled();
  });
});

describe('conflicting edits', () => {
  it('tells the user and reloads instead of retrying or overwriting', async () => {
    const update = jest.fn().mockResolvedValue(fail('CONFLICT'));
    renderSection({ update });
    await openEditor();
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }));

    expect(
      await screen.findByText(/changed somewhere else while you were editing/),
    ).toBeInTheDocument();
    // One attempt, no automatic retry: a retry would overwrite the other
    // writer's secret with no way to recover it.
    expect(update).toHaveBeenCalledTimes(1);
  });
});

describe('creating a record', () => {
  it('sends the chosen type', async () => {
    const create = jest.fn().mockResolvedValue(ok({ id: 'new' }));
    renderSection({ create });

    fireEvent.click(await screen.findByRole('button', { name: /New record/ }));
    await screen.findByText('New record');
    fireEvent.change(screen.getByPlaceholderText('What is this record for?'), {
      target: { value: 'Deploy key' },
    });
    fireEvent.change(screen.getByLabelText('Type'), {
      target: { value: 'note' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Create record/ }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(payloadOf(create).type).toBe('note');
    expect(payloadOf(create).name).toBe('Deploy key');
  });

  it('fills the password field from the daemon generator', async () => {
    const generate = jest.fn().mockResolvedValue(ok({ password: 'xyzzy-42' }));
    renderSection({ generate });

    fireEvent.click(await screen.findByRole('button', { name: /New record/ }));
    await screen.findByText('New record');
    fireEvent.click(screen.getByRole('button', { name: /Generate/ }));

    // Straight into the field: routing a fresh secret via the clipboard would
    // put it somewhere the user did not ask for it to go.
    expect(await screen.findByDisplayValue('xyzzy-42')).toBeInTheDocument();
    expect(generate).toHaveBeenCalled();
  });
});

describe('deleting a record', () => {
  it('requires a second, deliberate action', async () => {
    const remove = jest.fn().mockResolvedValue(ok({}));
    renderSection({ remove });

    fireEvent.click(await screen.findByRole('button', { name: 'Delete GitHub' }));
    // The first click only asks.
    expect(remove).not.toHaveBeenCalled();
    expect(screen.getByText(/This cannot be undone/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith('r1'));
  });

  it('leaves the record alone when the confirmation is cancelled', async () => {
    const remove = jest.fn().mockResolvedValue(ok({}));
    renderSection({ remove });

    fireEvent.click(await screen.findByRole('button', { name: 'Delete GitHub' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(remove).not.toHaveBeenCalled();
    expect(screen.queryByText(/This cannot be undone/)).not.toBeInTheDocument();
  });
});
