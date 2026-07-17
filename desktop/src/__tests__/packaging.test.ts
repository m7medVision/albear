/**
 * @jest-environment node
 */
import { execFileSync } from 'child_process';
import fs from 'fs';
import path from 'path';

const manifest = JSON.parse(
  fs.readFileSync(path.join(__dirname, '..', '..', 'package.json'), 'utf8'),
);
const build = manifest.build;

const REQUIRED_DEB_RUNTIME = [
  'libgtk-3-0',
  'libnotify4',
  'libnss3',
  'libxss1',
  'libxtst6',
  'xdg-utils',
  'libatspi2.0-0',
  'libuuid1',
  'libsecret-1-0',
];
function packageVersionForRelease(tag: string): string {
  const helper = path.join(
    __dirname,
    '..',
    '..',
    'scripts',
    'package-version.sh',
  );
  return execFileSync(
    'bash',
    [
      '-c',
      'source "$1"; package_version_for_release "$2"',
      'bash',
      helper,
      tag,
    ],
    { encoding: 'utf8' },
  ).trim();
}

const REQUIRED_RPM_RUNTIME = [
  'gtk3',
  'libnotify',
  'nss',
  'libXScrnSaver',
  '(libXtst or libXtst6)',
  'xdg-utils',
  'at-spi2-core',
  '(libuuid or libuuid1)',
];

describe('Linux desktop package configuration', () => {
  it('normalizes package versions without expanding tilde to the runner home', () => {
    expect(packageVersionForRelease('v0.2.0')).toBe('0.2.0');
    expect(packageVersionForRelease('v0.2.0-rc.1')).toBe('0.2.0~rc.1');
  });

  it('builds only x64 AppImage, deb, and rpm targets with distinct names', () => {
    expect(build.linux.target).toEqual([
      { target: 'AppImage', arch: ['x64'] },
      { target: 'deb', arch: ['x64'] },
      { target: 'rpm', arch: ['x64'] },
    ]);
    expect(build.linux.artifactName).toBe(
      'albear-desktop_${version}_${arch}.${ext}',
    );
    expect(build.linux.executableName).toBe('albear-desktop');
    expect(manifest.desktopName).toBe('dev.albear.desktop');
    expect(build.linux.syncDesktopName).toBe(true);
  });

  it('keeps Electron runtime dependencies when adding the core package', () => {
    expect(build.deb.packageName).toBe('albear-desktop');
    expect(build.rpm.packageName).toBe('albear-desktop');
    expect(build.deb.depends).toEqual(
      expect.arrayContaining(['albear', ...REQUIRED_DEB_RUNTIME]),
    );
    expect(build.rpm.depends).toEqual(
      expect.arrayContaining(['albear', ...REQUIRED_RPM_RUNTIME]),
    );
  });

  it('declares desktop integration without bundling core-owned resources', () => {
    expect(build.linux.icon).toBe('assets/icons');
    expect(build.linux.category).toBe('Utility');
    expect(build.linux.maintainer).toMatch(/<.+@.+>/);
    expect(build.extraResources).toEqual(['./assets/**']);
    expect(JSON.stringify(build)).not.toMatch(
      /vaultd|vault-native|albear-vaultd\.service/,
    );
  });
});
