/**
 * Proof-of-work primitives shared by the browser solver and its tests.
 */

import {sha256} from "@noble/hashes/sha256";

export const defaultPOWDifficulty = 16;

/**
 * Decode the server's fixed-width hexadecimal difficulty. Older servers do not
 * send the field, so an absent value retains the historic 16-bit target.
 *
 * @param {unknown} encoded
 * @return {number}
 */
export function parsePOWDifficulty(encoded){
    if (encoded === undefined || encoded === null){
        return defaultPOWDifficulty;
    }
    if (typeof encoded !== "string" || !/^[0-9a-f]{2}$/iu.test(encoded)){
        throw new Error("Invalid POW difficulty");
    }

    const difficulty = Number.parseInt(encoded, 16);
    if (difficulty < 1 || difficulty > 255){
        throw new Error("Invalid POW difficulty");
    }
    return difficulty;
}

/**
 * New challenges bind work to both authenticated fields so a nonce cannot be
 * reused with another client's signature. A missing difficulty identifies the
 * legacy wire format, whose work input contained only the random field.
 *
 * @param {{r: string, s: string}} challenge
 * @param {number} nonce
 * @return {string}
 */
export function powInput(challenge, nonce){
    if (challenge.d === undefined || challenge.d === null){
        return challenge.r + nonce.toString();
    }
    return challenge.r + challenge.s + nonce.toString();
}

/**
 * @param {string} data
 * @return {Promise<Uint8Array>}
 */
export async function doHash(data){
    const input = new TextEncoder().encode(data);

    if (import.meta.env?.VITE_NATIVE_CRYPTO === "true"){
        const hashBuffer = await crypto.subtle.digest("sha-256", input);
        return new Uint8Array(hashBuffer);
    }
    return sha256(input);
}

/**
 * @param {Uint8Array} bytes
 * @param {number} bits
 * @return {boolean}
 */
export function hasLeadingZeroBits(bytes, bits){
    if (!Number.isInteger(bits) || bits < 0 || bits > bytes.length * 8){
        return false;
    }

    const wholeBytes = Math.floor(bits / 8);
    for (let i = 0; i < wholeBytes; i++){
        if (bytes[i] !== 0){
            return false;
        }
    }

    const remainingBits = bits % 8;
    return remainingBits === 0 || (bytes[wholeBytes] >> (8 - remainingBits)) === 0;
}
