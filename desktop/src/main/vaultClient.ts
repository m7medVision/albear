// VaultClient: the desktop app's direct connection to the local vaultd
// daemon. Dials the Unix socket, runs the same-user CLI-mode Noise handshake
// (mirroring internal/client/client.go DialCLI: ephemeral static key +
// Hello{v:1, mode:"cli"}), then exchanges encrypted protocol envelopes.
//
// Main process only. The renderer never sees the socket or Noise state —
// it talks to this through ipcMain.handle (see ipc.ts).

import net from 'net';
import os from 'os';
import path from 'path';
import { CipherState, XXHandshake, generateKeyPair } from './noise';
import { FrameDecoder, encodeFrame } from './frames';
import {
  DAEMON_UNAVAILABLE,
  REQUEST_TIMEOUT,
  TRANSPORT_FAILED,
  WireResponse,
} from '../shared/vaultTypes';

export class VaultError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'VaultError';
  }
}

const PROTOCOL_VERSION = 1;
const HANDSHAKE_TIMEOUT_MS = 5_000;
const DEFAULT_TIMEOUT_MS = 10_000;

const te = new TextEncoder();
const td = new TextDecoder();
const EMPTY = new Uint8Array(0);

/**
 * Socket path resolution, mirroring
 * internal/infrastructure/system/paths.go ResolvePaths():
 * $XDG_RUNTIME_DIR/albear/vault.sock, falling back to
 * $XDG_DATA_HOME(~/.local/share)/albear/run/albear/vault.sock.
 */
export function defaultSocketPath(): string {
  const home = os.homedir();
  const data = process.env.XDG_DATA_HOME || path.join(home, '.local', 'share');
  const runtime =
    process.env.XDG_RUNTIME_DIR || path.join(data, 'albear', 'run');
  return path.join(runtime, 'albear', 'vault.sock');
}

interface FrameWaiter {
  resolve: (frame: Uint8Array) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export class VaultClient {
  private socket: net.Socket | null = null;

  private sendCipher: CipherState | null = null;

  private recvCipher: CipherState | null = null;

  private decoder = new FrameDecoder();

  private frames: Uint8Array[] = [];

  private waiter: FrameWaiter | null = null;

  private closedErr: Error | null = null;

  private counter = 0;

  // Requests are strictly serialized: the daemon answers in order on one
  // connection, and serializing means a response frame always belongs to
  // the single in-flight request.
  private chain: Promise<unknown> = Promise.resolve();

  private readonly socketPath: string;

  constructor(socketPath: string = defaultSocketPath()) {
    this.socketPath = socketPath;
  }

  /**
   * Sends one operation and returns the response data. Daemon failures and
   * connection problems reject with VaultError (code is a wire code or one
   * of DAEMON_UNAVAILABLE / TIMEOUT / TRANSPORT).
   */
  call<T>(
    operation: string,
    payload?: unknown,
    timeoutMs: number = DEFAULT_TIMEOUT_MS,
  ): Promise<T> {
    const run = () => this.doCall<T>(operation, payload, timeoutMs);
    const result = this.chain.then(run, run);
    this.chain = result.catch(() => undefined);
    return result;
  }

  close(): void {
    this.teardown(new VaultError(DAEMON_UNAVAILABLE, 'client closed'));
  }

  private async doCall<T>(
    operation: string,
    payload: unknown,
    timeoutMs: number,
  ): Promise<T> {
    if (!this.socket || !this.sendCipher || this.closedErr) {
      await this.connect();
    }
    this.counter += 1;
    const requestId = `desktop-${this.counter}`;
    const envelope: Record<string, unknown> = {
      protocolVersion: PROTOCOL_VERSION,
      requestId,
      operation,
    };
    if (payload !== undefined) envelope.payload = payload;

    const ciphertext = this.sendCipher!.encryptWithAd(
      EMPTY,
      te.encode(JSON.stringify(envelope)),
    );
    this.socket!.write(encodeFrame(ciphertext));

    const frame = await this.nextFrame(timeoutMs);
    let resp: WireResponse;
    try {
      const plaintext = this.recvCipher!.decryptWithAd(EMPTY, frame);
      resp = JSON.parse(td.decode(plaintext)) as WireResponse;
    } catch {
      // AEAD failure is fatal for the session (PRD 19.1 Level 2).
      this.teardown(
        new VaultError(TRANSPORT_FAILED, 'transport authentication failed'),
      );
      throw new VaultError(TRANSPORT_FAILED, 'transport authentication failed');
    }
    if (resp.requestId !== requestId) {
      this.teardown(
        new VaultError(TRANSPORT_FAILED, 'response for unknown request'),
      );
      throw new VaultError(TRANSPORT_FAILED, 'response for unknown request');
    }
    if (!resp.ok) {
      const e = resp.error ?? { code: 'INTERNAL', message: 'unknown failure' };
      throw new VaultError(e.code, e.message);
    }
    return resp.data as T;
  }

  private async connect(): Promise<void> {
    this.teardown(null); // drop any half-dead previous session

    const socket = await new Promise<net.Socket>((resolve, reject) => {
      const s = net.createConnection(this.socketPath);
      const onError = (err: Error) => {
        s.destroy();
        reject(
          new VaultError(
            DAEMON_UNAVAILABLE,
            `cannot reach vaultd at ${this.socketPath}: ${err.message}`,
          ),
        );
      };
      s.once('error', onError);
      s.once('connect', () => {
        s.removeListener('error', onError);
        resolve(s);
      });
    });

    this.socket = socket;
    this.closedErr = null;
    this.decoder = new FrameDecoder();
    this.frames = [];

    socket.on('data', (chunk: Buffer) => {
      try {
        for (const frame of this.decoder.push(chunk)) this.deliver(frame);
      } catch (err) {
        this.teardown(
          new VaultError(
            TRANSPORT_FAILED,
            err instanceof Error ? err.message : 'malformed frame',
          ),
        );
      }
    });
    socket.on('error', (err: Error) => {
      this.teardown(
        new VaultError(DAEMON_UNAVAILABLE, `vaultd connection: ${err.message}`),
      );
    });
    socket.on('close', () => {
      this.teardown(
        new VaultError(DAEMON_UNAVAILABLE, 'vaultd closed the connection'),
      );
    });

    // CLI-mode handshake: plaintext hello (doubles as the Noise prologue),
    // then Noise_XX with a fresh ephemeral static key. The daemon authorizes
    // "cli" mode via peer credentials on the socket, not via our key.
    const helloRaw = te.encode(JSON.stringify({ v: 1, mode: 'cli' }));
    const hs = new XXHandshake({
      staticKey: generateKeyPair(),
      prologue: helloRaw,
    });
    try {
      socket.write(encodeFrame(helloRaw));
      socket.write(encodeFrame(hs.writeMessageA()));
      hs.readMessageB(await this.nextFrame(HANDSHAKE_TIMEOUT_MS));
      socket.write(encodeFrame(hs.writeMessageC()));
    } catch (err) {
      const wrapped =
        err instanceof VaultError
          ? err
          : new VaultError(
              TRANSPORT_FAILED,
              `noise handshake failed: ${err instanceof Error ? err.message : String(err)}`,
            );
      this.teardown(wrapped);
      throw wrapped;
    }
    const { send, recv } = hs.split();
    this.sendCipher = send;
    this.recvCipher = recv;
  }

  private deliver(frame: Uint8Array): void {
    if (this.waiter) {
      const w = this.waiter;
      this.waiter = null;
      clearTimeout(w.timer);
      w.resolve(frame);
    } else {
      this.frames.push(frame);
    }
  }

  private nextFrame(timeoutMs: number): Promise<Uint8Array> {
    const buffered = this.frames.shift();
    if (buffered) return Promise.resolve(buffered);
    if (this.closedErr) return Promise.reject(this.closedErr);
    return new Promise<Uint8Array>((resolve, reject) => {
      const timer = setTimeout(() => {
        // The connection is in an unknown state after a timeout: drop it.
        this.teardown(
          new VaultError(REQUEST_TIMEOUT, 'vaultd did not respond in time'),
        );
      }, timeoutMs);
      this.waiter = { resolve, reject, timer };
    });
  }

  private teardown(err: Error | null): void {
    if (this.socket) {
      this.socket.removeAllListeners();
      this.socket.destroy();
      this.socket = null;
    }
    this.sendCipher = null;
    this.recvCipher = null;
    this.frames = [];
    this.closedErr = err;
    if (this.waiter) {
      const w = this.waiter;
      this.waiter = null;
      clearTimeout(w.timer);
      w.reject(err ?? new VaultError(DAEMON_UNAVAILABLE, 'connection closed'));
    }
  }
}
