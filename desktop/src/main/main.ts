/* eslint global-require: off, no-console: off, promise/always-return: off */

/**
 * This module executes inside of electron's main process. You can start
 * electron renderer process from here and communicate with the other processes
 * through IPC.
 *
 * When running `npm run build` or `npm run build:main`, this file is compiled to
 * `./src/main.js` using webpack. This gives us some performance wins.
 */
import path from 'path';
import { app, BrowserWindow, shell, ipcMain } from 'electron';
import { autoUpdater } from 'electron-updater';
import log from 'electron-log';
import MenuBuilder from './menu';
import { resolveHtmlPath } from './util';
import { VaultClient } from './vaultClient';
import { registerVaultIpc } from './ipc';

/**
 * Auto-updates. Checks GitHub releases (electron-builder `publish` config),
 * forwards progress to the renderer over IPC, and does nothing at all when
 * the app is not packaged (dev builds have no update feed).
 */
class AppUpdater {
  constructor(window: BrowserWindow) {
    // Fail silently in development / unpackaged builds.
    if (!app.isPackaged) return;

    log.transports.file.level = 'info';
    autoUpdater.logger = log;
    autoUpdater.autoDownload = true;

    const send = (channel: string, payload: unknown) => {
      if (!window.isDestroyed()) {
        window.webContents.send(channel, payload);
      }
    };

    autoUpdater.on('update-available', (info) => {
      send('updater:update-available', { version: info.version });
    });
    autoUpdater.on('download-progress', (progress) => {
      send('updater:download-progress', {
        percent: progress.percent,
        transferred: progress.transferred,
        total: progress.total,
        bytesPerSecond: progress.bytesPerSecond,
      });
    });
    autoUpdater.on('update-downloaded', (info) => {
      send('updater:update-downloaded', { version: info.version });
    });
    // Never surface updater failures to the user; log and move on.
    autoUpdater.on('error', (err) => {
      log.warn('auto-update failed (ignored):', err);
    });

    autoUpdater.checkForUpdatesAndNotify().catch((err) => {
      log.warn('auto-update check failed (ignored):', err);
    });
  }
}

let mainWindow: BrowserWindow | null = null;

// One shared connection to the local vaultd daemon. It dials lazily on the
// first request and reconnects on demand, so construction is safe here.
const vaultClient = new VaultClient();
registerVaultIpc(vaultClient);

ipcMain.on('updater:quit-and-install', () => {
  if (app.isPackaged) {
    autoUpdater.quitAndInstall();
  }
});

if (process.env.NODE_ENV === 'production') {
  const sourceMapSupport = require('source-map-support');
  sourceMapSupport.install();
}

const isDebug =
  process.env.NODE_ENV === 'development' || process.env.DEBUG_PROD === 'true';

if (isDebug) {
  require('electron-debug').default();
}

const createWindow = async () => {
  const RESOURCES_PATH = app.isPackaged
    ? path.join(process.resourcesPath, 'assets')
    : path.join(__dirname, '../../assets');

  const getAssetPath = (...paths: string[]): string => {
    return path.join(RESOURCES_PATH, ...paths);
  };

  mainWindow = new BrowserWindow({
    show: false,
    width: 1024,
    height: 728,
    icon: getAssetPath('icon.png'),
    webPreferences: {
      preload: app.isPackaged
        ? path.join(__dirname, 'preload.js')
        : path.join(__dirname, '../../.erb/dll/preload.js'),
    },
  });

  mainWindow.loadURL(resolveHtmlPath('index.html'));

  mainWindow.on('ready-to-show', () => {
    if (!mainWindow) {
      throw new Error('"mainWindow" is not defined');
    }
    if (process.env.START_MINIMIZED) {
      mainWindow.minimize();
    } else {
      mainWindow.show();
    }
  });

  mainWindow.on('closed', () => {
    mainWindow = null;
  });

  const menuBuilder = new MenuBuilder(mainWindow);
  menuBuilder.buildMenu();

  // Open urls in the user's browser
  mainWindow.webContents.setWindowOpenHandler((edata) => {
    shell.openExternal(edata.url);
    return { action: 'deny' };
  });

  // eslint-disable-next-line no-new
  new AppUpdater(mainWindow);
};

/**
 * Add event listeners...
 */

app.on('quit', () => {
  vaultClient.close();
});

app.on('window-all-closed', () => {
  // Respect the OSX convention of having the application in memory even
  // after all windows have been closed
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

app
  .whenReady()
  .then(() => {
    createWindow();
    app.on('activate', () => {
      // On macOS it's common to re-create a window in the app when the
      // dock icon is clicked and there are no other windows open.
      if (mainWindow === null) createWindow();
    });
  })
  .catch(console.log);
