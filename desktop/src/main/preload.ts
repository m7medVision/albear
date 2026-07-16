// Disable no-unused-vars, broken for spread args
/* eslint no-unused-vars: off */
import { contextBridge, ipcRenderer, IpcRendererEvent } from 'electron';
import type {
  AlbearResult,
  DesktopStatus,
  GenerateOptions,
  RecordView,
  SecretView,
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
  generate: (
    options?: GenerateOptions,
  ): Promise<AlbearResult<{ password: string }>> =>
    ipcRenderer.invoke('albear:generate', options),
  copyText: (text: string): Promise<AlbearResult<unknown>> =>
    ipcRenderer.invoke('albear:copy', text),
};

contextBridge.exposeInMainWorld('albear', albearHandler);

export type AlbearHandler = typeof albearHandler;
