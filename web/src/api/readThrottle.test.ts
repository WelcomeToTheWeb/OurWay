import { describe, expect, it } from 'vitest';
import {
  MAX_CONCURRENT_READS,
  acquireReadSlot,
  pendingReads,
  releaseReadSlot,
} from './client';

/**
 * The server limits each user to 100 requests per minute with an
 * instantaneous burst of 10. A dashboard mount issues six-plus reads at
 * once and the harvest poll adds more, so unthrottled mounts used to
 * return 429 panels. These tests pin the throttle that keeps a page
 * painting fully: reads cap at MAX_CONCURRENT_READS, a finished read hands
 * its slot to the oldest queued read (so a slot is never lost), and an
 * empty queue drains the active count.
 */
describe('API read throttle', () => {
  it('caps concurrent reads and queues the rest', async () => {
    const acquired: number[] = [];
    for (let i = 0; i < MAX_CONCURRENT_READS; i += 1) {
      await acquireReadSlot();
      acquired.push(i);
    }
    expect(acquired.length).toBe(MAX_CONCURRENT_READS);
    expect(pendingReads()).toEqual({ active: MAX_CONCURRENT_READS, queued: 0 });

    let queuedResolved = false;
    const queued = acquireReadSlot().then(() => {
      queuedResolved = true;
    });

    // Let any microtask settle: the queued read must still be waiting.
    await Promise.resolve();
    await Promise.resolve();
    expect(queuedResolved).toBe(false);
    expect(pendingReads().queued).toBe(1);

    releaseReadSlot();
    await queued;
    expect(queuedResolved).toBe(true);
    // The released slot was handed to the queued read, so the active count
    // never dropped below the cap.
    expect(pendingReads()).toEqual({ active: MAX_CONCURRENT_READS, queued: 0 });

    releaseReadSlot();
    expect(pendingReads()).toEqual({ active: MAX_CONCURRENT_READS - 1, queued: 0 });
  });

  it('frees a slot when nothing is queued', async () => {
    const before = pendingReads().active;
    await acquireReadSlot();
    expect(pendingReads().active).toBe(before + 1);
    releaseReadSlot();
    expect(pendingReads()).toEqual({ active: before, queued: 0 });
  });
});
