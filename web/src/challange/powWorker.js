/**
 * Web Worker POW solver. Solving off the main thread is what the "pow-worker"
 * challenge type requires; the produced solution is identical to a main-thread
 * solve.
 */

import {doHash, hasLeadingZeroBits} from "./pow";

self.onmessage = async(e) => {
    const {r, d} = e.data;

    // eslint-disable-next-line no-constant-condition
    for (let i = 0; true; i++){
        const hash = await doHash(r + i.toString());
        if (hasLeadingZeroBits(hash, d)){
            self.postMessage(i);
            return;
        }
    }
};
