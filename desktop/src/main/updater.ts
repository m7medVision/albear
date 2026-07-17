// Package ownership decides who may replace the desktop executable. AppImage
// is self-contained and electron-updater owns it; deb/rpm files are owned by
// the system package manager and must never be overwritten by an AppImage.

export function shouldUseDesktopAutoUpdater(
  isPackaged: boolean,
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  return isPackaged && typeof env.APPIMAGE === 'string' && env.APPIMAGE.length > 0;
}
