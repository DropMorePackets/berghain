import assert from "node:assert/strict";
import test from "node:test";

import {getChallengeSolver, solvePOWWithWorker} from "./challanges.js";
import {
    defaultPOWDifficulty,
    doHash,
    hasLeadingZeroBits,
    parsePOWDifficulty,
    powInput,
    solvePOWNonce,
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

test("main-thread nonce solver uses the signed POW input", async() => {
    const challenge = {d: "04", r: "timestamp", s: "signature"};
    const nonce = await solvePOWNonce(challenge);
    const digest = await doHash(powInput(challenge, nonce));
    assert.equal(hasLeadingZeroBits(digest, 4), true);
});

test("worker solver sends the complete challenge and terminates", async() => {
    let instance;

    class FakeWorker {
        constructor(){
            instance = this;
            this.terminated = false;
        }

        postMessage(challenge){
            this.challenge = challenge;
            queueMicrotask(() => this.onmessage({data: {nonce: 7}}));
        }

        terminate(){
            this.terminated = true;
        }
    }

    const challenge = {d: "04", r: "timestamp", s: "signed-value"};
    assert.equal(await solvePOWWithWorker(challenge, FakeWorker), 7);
    assert.deepEqual(instance.challenge, challenge);
    assert.equal(instance.terminated, true);
    assert.match(getChallengeSolver(2)[0], /Worker/u);
});

test("worker solver propagates errors and terminates", async() => {
    let instance;

    class FakeWorker {
        constructor(){
            instance = this;
            this.terminated = false;
        }

        postMessage(){
            queueMicrotask(() => this.onmessage({data: {error: "hashing unavailable"}}));
        }

        terminate(){
            this.terminated = true;
        }
    }

    await assert.rejects(
        solvePOWWithWorker({d: "04", r: "timestamp", s: "signature"}, FakeWorker),
        /POW worker failed: hashing unavailable/u,
    );
    assert.equal(instance.terminated, true);
});
