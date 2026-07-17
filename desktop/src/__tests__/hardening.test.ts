/**
 * @jest-environment node
 */
// Renderer lockdown: the CSP that ships and the external-link allowlist.
//
// A revealed secret is in the renderer's memory and DOM, so the renderer is
// where an exfiltration bug would pay off. These tests pin the two things that
// stop it: no network egress, and no navigating the window somewhere else.
import fs from 'fs';
import path from 'path';
import { isSafeExternalUrl } from '../main/util';
import { MAX_FRAME_SIZE } from '../main/frames';

// The template is EJS with a dev branch and a packaged branch; read the file
// rather than the built output so the test does not need a webpack run.
const template = fs.readFileSync(
  path.join(__dirname, '..', 'renderer', 'index.ejs'),
  'utf8',
);

/** The CSP from the packaged (non-development) branch of the template. */
function packagedCsp(): string {
  const branches = [...template.matchAll(/content="([^"]*default-src[^"]*)"/g)];
  if (branches.length !== 2) {
    throw new Error(
      `expected a dev and a packaged CSP, found ${branches.length}`,
    );
  }
  // The packaged branch is the second: dev first, then the `else`.
  return branches[1]![1]!;
}

describe('packaged renderer CSP', () => {
  it('denies everything by default', () => {
    expect(packagedCsp()).toContain("default-src 'none'");
  });

  it('denies all network egress', () => {
    // The point of the exercise: a compromised renderer holding a revealed
    // secret must have nowhere to send it. No fetch, XHR, WebSocket or beacon.
    expect(packagedCsp()).toContain("connect-src 'none'");
  });

  it('allows no remote host anywhere', () => {
    const csp = packagedCsp();
    expect(csp).not.toMatch(/https?:\/\//);
    expect(csp).not.toContain('*');
    expect(csp).not.toContain('unsafe-eval');
  });

  it('permits no inline script', () => {
    // Everything is bundled, so 'self' is enough; inline script is the usual
    // way an injected record field becomes code.
    expect(packagedCsp()).toContain("script-src 'self'");
    expect(packagedCsp()).not.toContain("script-src 'self' 'unsafe-inline'");
  });

  it('blocks framing, form posts and base-tag rewrites', () => {
    const csp = packagedCsp();
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("form-action 'none'");
    expect(csp).toContain("base-uri 'none'");
    expect(csp).toContain("object-src 'none'");
  });

  it('keeps the development branch confined to localhost', () => {
    const dev = [...template.matchAll(/content="([^"]*default-src[^"]*)"/g)][0]![1]!;
    expect(dev).toContain("default-src 'none'");
    // Dev needs the HMR socket, but still nothing off-machine.
    expect(dev).toMatch(/connect-src 'self' ws:\/\/localhost:\* http:\/\/localhost:\*/);
    expect(dev).not.toMatch(/https?:\/\/(?!localhost)/);
  });
});

describe('isSafeExternalUrl', () => {
  it('allows ordinary web links', () => {
    expect(isSafeExternalUrl('https://example.com/x')).toBe(true);
    expect(isSafeExternalUrl('http://example.com')).toBe(true);
    expect(isSafeExternalUrl('mailto:someone@example.com')).toBe(true);
  });

  it('refuses schemes that reach the local machine or the network stack', () => {
    // shell.openExternal hands these to the OS handler.
    expect(isSafeExternalUrl('file:///etc/passwd')).toBe(false);
    expect(isSafeExternalUrl('smb://attacker.example/share')).toBe(false);
    expect(isSafeExternalUrl('javascript:alert(1)')).toBe(false);
    expect(isSafeExternalUrl('data:text/html,<script>x</script>')).toBe(false);
    expect(isSafeExternalUrl('vbscript:msgbox')).toBe(false);
  });

  it('fails closed on anything it cannot parse', () => {
    expect(isSafeExternalUrl('')).toBe(false);
    expect(isSafeExternalUrl('not a url')).toBe(false);
    expect(isSafeExternalUrl('///')).toBe(false);
  });
});

describe('frame ceiling', () => {
  it('matches MaxFrameSize in frames.go', () => {
    // Go is the source of truth for the wire format; a mismatch here means
    // producing frames vaultd rejects. See internal/infrastructure/transport/
    // noise/frames.go.
    const go = fs.readFileSync(
      path.join(
        __dirname,
        '..',
        '..',
        '..',
        'internal',
        'infrastructure',
        'transport',
        'noise',
        'frames.go',
      ),
      'utf8',
    );
    const match = go.match(/const MaxFrameSize = (\d+) \* 1024/);
    expect(match).not.toBeNull();
    expect(MAX_FRAME_SIZE).toBe(Number(match![1]) * 1024);
  });
});
