/**
 * Shared proof-of-work primitives, used by both the main-thread solver and the
 * Web Worker solver so their hashing and difficulty checks stay identical.
 */

import {sha256} from "@noble/hashes/sha256";

/**
 * Hash data with SHA-256 and return the raw digest bytes.
 *
 * @param {string} data
 * @return {Promise<Uint8Array>}
 */
export async function doHash(data){
    const input = new TextEncoder().encode(data);

    if (import.meta.env.VITE_NATIVE_CRYPTO === "true"){
        const hashBuffer = await crypto.subtle.digest("sha-256", input);
        return new Uint8Array(hashBuffer);
    }
    return sha256(input);
}

/**
 * Check whether the first `bits` bits of `bytes` are all zero.
 * Mirror of the Go server-side hasLeadingZeroBits.
 *
 * @param {Uint8Array} bytes
 * @param {number} bits
 * @return {boolean}
 */
export function hasLeadingZeroBits(bytes, bits){
    const n = bits >> 3;
    for (let i = 0; i < n; i++){
        if (bytes[i] !== 0){
            return false;
        }
    }
    const r = bits & 7;
    if (r !== 0 && (bytes[n] >> (8 - r)) !== 0){
        return false;
    }
    return true;
}
