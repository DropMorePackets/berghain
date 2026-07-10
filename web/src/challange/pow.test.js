import assert from "node:assert/strict";
import test from "node:test";

import {getChallengeSolver} from "./challanges.js";
import {
    defaultPOWDifficulty,
    doHash,
    hasLeadingZeroBits,
    parsePOWDifficulty,
    powInput,
} from "./pow.js";

test("difficulty defaults to the historic target", () => {
    assert.equal(parsePOWDifficulty(undefined), defaultPOWDifficulty);
    assert.equal(parsePOWDifficulty("01"), 1);
    assert.equal(parsePOWDifficulty("ff"), 255);
    assert.throws(() => parsePOWDifficulty("00"), /Invalid POW difficulty/u);
    assert.throws(() => parsePOWDifficulty("100"), /Invalid POW difficulty/u);
});

test("leading zero bits supports partial bytes and rejects out-of-range input", () => {
    assert.equal(hasLeadingZeroBits(Uint8Array.of(0x00, 0x0f), 12), true);
    assert.equal(hasLeadingZeroBits(Uint8Array.of(0x00, 0x10), 12), false);
    assert.equal(hasLeadingZeroBits(Uint8Array.of(0x00), 9), false);
});

test("work input includes the authenticated challenge signature", () => {
    const challenge = {d: "10", r: "timestamp", s: "signature-a"};
    assert.equal(powInput(challenge, 7), "timestampsignature-a7");
    assert.notEqual(powInput(challenge, 7), powInput({...challenge, s: "signature-b"}, 7));
});

test("a challenge without difficulty uses the legacy work input", () => {
    assert.equal(powInput({r: "timestamp", s: "signature"}, 7), "timestamp7");
});

test("a non-successful solution submission rejects", async(t) => {
    const originalFetch = globalThis.fetch;
    t.after(() => {
        globalThis.fetch = originalFetch;
    });
    globalThis.fetch = async() => ({ok: false, status: 429});

    const [, solve] = getChallengeSolver(1);
    await assert.rejects(
        solve({d: "01", r: "timestamp", s: "signature"}),
        /Challenge submission failed \(429\)/u,
    );
});

test("hashing produces a SHA-256 digest", async() => {
    const digest = await doHash("proof");
    assert.equal(digest.length, 32);
});
