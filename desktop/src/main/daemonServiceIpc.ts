import { ipcMain } from 'electron';
import type { AlbearResult, DaemonServiceStatus } from '../shared/vaultTypes';
import {
  DaemonServiceController,
  DaemonServiceError,
} from './daemonService';

interface ServiceController {
  status(): Promise<DaemonServiceStatus>;
  setup(): Promise<DaemonServiceStatus>;
}

async function toResult(
  work: () => Promise<DaemonServiceStatus>,
): Promise<AlbearResult<DaemonServiceStatus>> {
  try {
    return { ok: true, data: await work() };
  } catch (error) {
    if (error instanceof DaemonServiceError) {
      return {
        ok: false,
        error: { code: error.code, message: error.message },
      };
    }
    return {
      ok: false,
      error: { code: 'INTERNAL', message: 'unexpected service-control failure' },
    };
  }
}

export function registerDaemonServiceIpc(
  controller: ServiceController = new DaemonServiceController(),
): void {
  ipcMain.handle('albear:daemonServiceStatus', () =>
    toResult(() => controller.status()),
  );
  ipcMain.handle('albear:daemonServiceSetup', () =>
    toResult(() => controller.setup()),
  );
}
