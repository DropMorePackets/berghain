import assert from "node:assert/strict";
import test from "node:test";

import {detectMissingCapabilities} from "../src/challange/capabilities.js";

const completeEnvironment = {
    TextEncoder: class {},
    Worker: class {},
    crypto: {subtle: {}},
};

test("does not probe capabilities unused by a no-op challenge", () => {
    assert.deepEqual(detectMissingCapabilities(0, {
        environment: {},
        nativeCrypto: true,
    }), []);
});

test("requires only text encoding for bundled-crypto POW", () => {
    assert.deepEqual(detectMissingCapabilities(1, {
        environment: completeEnvironment,
        nativeCrypto: false,
    }), []);

    const missing = detectMissingCapabilities(1, {
        environment: {},
        nativeCrypto: false,
    });
    assert.deepEqual(missing.map(({name}) => name), ["Text encoding"]);
});

test("requires Web Crypto only in the native build", () => {
    const missing = detectMissingCapabilities(1, {
        environment: {TextEncoder: class {}},
        nativeCrypto: true,
    });
    assert.deepEqual(missing.map(({name}) => name), ["Web Crypto"]);
});

test("requires a Worker only for worker POW", () => {
    const environment = {...completeEnvironment, Worker: undefined};
    const missing = detectMissingCapabilities(2, {
        environment,
        nativeCrypto: false,
    });
    assert.deepEqual(missing.map(({name}) => name), ["Web Workers"]);
});
