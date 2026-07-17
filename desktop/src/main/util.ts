/* eslint import/prefer-default-export: off */
import { URL } from 'url';
import path from 'path';

/**
 * Whether a URL may be handed to the user's browser. An allowlist, so an
 * unknown or malformed scheme fails closed: `shell.openExternal` delegates to
 * the OS handler, where `file://` opens local content and schemes such as
 * `smb://` can trigger an outbound connection.
 */
export function isSafeExternalUrl(raw: string): boolean {
  try {
    const { protocol } = new URL(raw);
    return (
      protocol === 'https:' || protocol === 'http:' || protocol === 'mailto:'
    );
  } catch {
    return false;
  }
}

export function resolveHtmlPath(htmlFileName: string) {
  if (process.env.NODE_ENV === 'development') {
    const port = process.env.PORT || 1212;
    const url = new URL(`http://localhost:${port}`);
    url.pathname = htmlFileName;
    return url.href;
  }
  return `file://${path.resolve(__dirname, '../renderer/', htmlFileName)}`;
}
