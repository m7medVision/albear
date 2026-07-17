// Disable no-unused-vars, broken for spread args
/* eslint no-unused-vars: off */
import { contextBridge, ipcRenderer, IpcRendererEvent } from 'electron';
import type {
  AlbearResult,
  BackupInfo,
  BackupResult,
  Canceled,
  ClientView,
  DesktopStatus,
  DaemonServiceStatus,
  EventView,
  GenerateOptions,
  PendingPairingView,
  RecordFields,
  RecordView,
  Restored,
  SecretView,
  UpdateFields,
} from '../shared/vaultTypes';

export type Channels =
  | 'updater:update-available'
  | 'updater:download-progress'
  | 'updater:update-downloaded'
  | 'updater:quit-and-install';

const electronHandler = {
  ipcRenderer: {
    sendMessage(channel: Channels, ...args: unknown[]) {
      ipcRenderer.send(channel, ...args);
    },
    on(channel: Channels, func: (...args: unknown[]) => void) {
      const subscription = (_event: IpcRendererEvent, ...args: unknown[]) =>
        func(...args);
      ipcRenderer.on(channel, subscription);

      return () => {
        ipcRenderer.removeListener(channel, subscription);
      };
    },
    once(channel: Channels, func: (...args: unknown[]) => void) {
      ipcRenderer.once(channel, (_event, ...args) => func(...args));
    },
  },
};

contextBridge.exposeInMainWorld('electron', electronHandler);

export type ElectronHandler = typeof electronHandler;

// Typed vault API. Only plain data crosses this bridge; the socket and the
// Noise session stay in the main process (see src/main/vaultClient.ts).
const albearHandler = {
  status: (): Promise<AlbearResult<DesktopStatus>> =>
    ipcRenderer.invoke('albear:status'),
  daemonServiceStatus: (): Promise<AlbearResult<DaemonServiceStatus>> =>
    ipcRenderer.invoke('albear:daemonServiceStatus'),
  daemonServiceSetup: (): Promise<AlbearResult<DaemonServiceStatus>> =>
    ipcRenderer.invoke('albear:daemonServiceSetup'),
  unlock: (password: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:unlock', password),
  lock: (): Promise<AlbearResult<unknown>> => ipcRenderer.invoke('albear:lock'),
  list: (): Promise<AlbearResult<{ records: RecordView[] }>> =>
    ipcRenderer.invoke('albear:list'),
  search: (query: string): Promise<AlbearResult<{ records: RecordView[] }>> =>
    ipcRenderer.invoke('albear:search', query),
  show: (ref: string): Promise<AlbearResult<RecordView>> =>
    ipcRenderer.invoke('albear:show', ref),
  reveal: (ref: string): Promise<AlbearResult<SecretView>> =>
    ipcRenderer.invoke('albear:reveal', ref),
  create: (fields: RecordFields): Promise<AlbearResult<{ id: string }>> =>
    ipcRenderer.invoke('albear:create', fields),
  // The daemon replaces the record rather than patching it, so `fields` must
  // carry every secret the record should keep — reveal before building it.
  update: (fields: UpdateFields): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:update', fields),
  remove: (id: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:delete', id),
  generate: (
    options?: GenerateOptions,
  ): Promise<AlbearResult<{ password: string }>> =>
    ipcRenderer.invoke('albear:generate', options),
  copyText: (text: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:copy', text),
  init: (password: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:init', password),
  changePassword: (
    current: string,
    next: string,
  ): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:changePassword', current, next),
  panic: (): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:panic'),
  clientsPending: (): Promise<AlbearResult<{ pending: PendingPairingView[] }>> =>
    ipcRenderer.invoke('albear:clientsPending'),
  clientsList: (): Promise<AlbearResult<{ clients: ClientView[] }>> =>
    ipcRenderer.invoke('albear:clientsList'),
  clientsApprove: (pairingId: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:clientsApprove', pairingId),
  clientsRevoke: (id: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:clientsRevoke', id),
  events: (limit?: number): Promise<AlbearResult<{ events: EventView[] }>> =>
    ipcRenderer.invoke('albear:events', limit),
  // The three backup calls take no path: main opens the dialog and keeps the
  // chosen path to itself. A `canceled` result means the user dismissed the
  // dialog and is a success, not an error.
  backupCreate: (): Promise<AlbearResult<BackupResult | Canceled>> =>
    ipcRenderer.invoke('albear:backupCreate'),
  backupVerify: (): Promise<AlbearResult<BackupInfo | Canceled>> =>
    ipcRenderer.invoke('albear:backupVerify'),
  backupRestore: (): Promise<AlbearResult<Restored | Canceled>> =>
    ipcRenderer.invoke('albear:backupRestore'),
};

contextBridge.exposeInMainWorld('albear', albearHandler);

export type AlbearHandler = typeof albearHandler;
