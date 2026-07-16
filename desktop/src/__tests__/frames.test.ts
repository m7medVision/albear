/**
 * @jest-environment node
 */
// Frame codec tests: the desktop client must produce and parse the exact
// length-prefixed layout of internal/infrastructure/transport/noise/frames.go.
import {
  FrameDecoder,
  FrameError,
  MAX_FRAME_SIZE,
  encodeFrame,
} from '../main/frames';

describe('encodeFrame', () => {
  it('writes a 4-byte big-endian length prefix', () => {
    const frame = encodeFrame(new Uint8Array([0xaa, 0xbb, 0xcc]));
    expect([...frame]).toEqual([0, 0, 0, 3, 0xaa, 0xbb, 0xcc]);
  });

  it('encodes an empty payload', () => {
    expect([...encodeFrame(new Uint8Array(0))]).toEqual([0, 0, 0, 0]);
  });

  it('rejects oversized payloads', () => {
    expect(() => encodeFrame(new Uint8Array(MAX_FRAME_SIZE + 1))).toThrow(
      FrameError,
    );
  });
});

describe('FrameDecoder', () => {
  it('round-trips a frame', () => {
    const decoder = new FrameDecoder();
    const payload = new Uint8Array([1, 2, 3, 4, 5]);
    const frames = decoder.push(encodeFrame(payload));
    expect(frames).toHaveLength(1);
    expect([...frames[0]!]).toEqual([...payload]);
  });

  it('reassembles frames split across arbitrary chunk boundaries', () => {
    const payload = new Uint8Array(300).map((_, i) => i % 251);
    const wire = encodeFrame(payload);
    for (let split = 1; split < 8; split += 1) {
      const decoder = new FrameDecoder();
      const collected: Uint8Array[] = [];
      for (let off = 0; off < wire.length; off += split) {
        collected.push(...decoder.push(wire.subarray(off, off + split)));
      }
      expect(collected).toHaveLength(1);
      expect([...collected[0]!]).toEqual([...payload]);
    }
  });

  it('parses multiple frames from one chunk', () => {
    const decoder = new FrameDecoder();
    const wire = Buffer.concat([
      encodeFrame(new Uint8Array([1])),
      encodeFrame(new Uint8Array([2, 2])),
      encodeFrame(new Uint8Array(0)),
    ]);
    const frames = decoder.push(wire);
    expect(frames.map((f) => f.length)).toEqual([1, 2, 0]);
    expect(frames[0]![0]).toBe(1);
    expect([...frames[1]!]).toEqual([2, 2]);
  });

  it('rejects a frame header exceeding the size limit before buffering', () => {
    const decoder = new FrameDecoder();
    const hdr = Buffer.alloc(4);
    hdr.writeUInt32BE(MAX_FRAME_SIZE + 1, 0);
    expect(() => decoder.push(hdr)).toThrow(FrameError);
  });

  it('returns copies detached from the shared receive buffer', () => {
    const decoder = new FrameDecoder();
    const [frame] = decoder.push(encodeFrame(new Uint8Array([7])));
    decoder.push(encodeFrame(new Uint8Array([9])));
    expect(frame![0]).toBe(7);
  });
});
