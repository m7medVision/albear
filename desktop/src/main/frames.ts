// Length-prefixed framing over the vaultd Unix socket, mirroring
// internal/infrastructure/transport/noise/frames.go: 4-byte big-endian
// length followed by the payload, with the same hard size cap.

// Must equal MaxFrameSize in frames.go. The desktop app reaches the daemon
// directly and never crosses native messaging, but the daemon enforces one
// ceiling for every transport, so a larger value here would only produce
// frames vaultd rejects.
export const MAX_FRAME_SIZE = 750 * 1024;

export class FrameError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'FrameError';
  }
}

/** Encodes one frame (header + payload) ready to write to the socket. */
export function encodeFrame(payload: Uint8Array): Buffer {
  if (payload.length > MAX_FRAME_SIZE) {
    throw new FrameError('frame exceeds maximum size');
  }
  const buf = Buffer.allocUnsafe(4 + payload.length);
  buf.writeUInt32BE(payload.length, 0);
  buf.set(payload, 4);
  return buf;
}

/**
 * Incremental frame parser: feed raw socket chunks, get back complete
 * frames. Enforces the size limit before buffering a frame body, exactly
 * like the Go ReadFrame.
 */
export class FrameDecoder {
  private buffer: Buffer = Buffer.alloc(0);

  /** Appends a chunk and returns every frame completed by it. */
  push(chunk: Buffer): Uint8Array[] {
    this.buffer =
      this.buffer.length === 0 ? chunk : Buffer.concat([this.buffer, chunk]);
    const frames: Uint8Array[] = [];
    for (;;) {
      if (this.buffer.length < 4) break;
      const n = this.buffer.readUInt32BE(0);
      if (n > MAX_FRAME_SIZE) {
        throw new FrameError('frame exceeds maximum size');
      }
      if (this.buffer.length < 4 + n) break;
      // Copy out: the shared backing buffer is reused for future chunks.
      frames.push(new Uint8Array(this.buffer.subarray(4, 4 + n)));
      this.buffer = this.buffer.subarray(4 + n);
    }
    return frames;
  }
}
