/**
 * Inline Web Worker entry point for the pow-worker challenge type.
 */

import {solvePOWNonce} from "./pow.js";

self.onmessage = async(event) => {
    try {
        const nonce = await solvePOWNonce(event.data);
        self.postMessage({nonce});
    }
    catch (error){
        const message = error instanceof Error ? error.message : "Unknown POW worker error";
        self.postMessage({error: message});
    }
};
