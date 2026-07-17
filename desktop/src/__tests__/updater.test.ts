/**
 * @jest-environment node
 */
import { shouldUseDesktopAutoUpdater } from '../main/updater';

describe('desktop update ownership', () => {
  it('enables self-update only for a packaged AppImage', () => {
    expect(
      shouldUseDesktopAutoUpdater(true, {
        APPIMAGE: '/tmp/Albear.AppImage',
      }),
    ).toBe(true);
  });

  it('disables self-update in development', () => {
    expect(
      shouldUseDesktopAutoUpdater(false, {
        APPIMAGE: '/tmp/Albear.AppImage',
      }),
    ).toBe(false);
  });

  it('does not replace deb or rpm package-manager files', () => {
    expect(shouldUseDesktopAutoUpdater(true, {})).toBe(false);
    expect(shouldUseDesktopAutoUpdater(true, { APPIMAGE: '' })).toBe(false);
  });
});
