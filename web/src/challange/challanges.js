/**
 * Collection of challenges.
 */

import shajs from 'sha.js'

/**
 * Calculate native hash.
 *
 * @param {string} data
 * @param {AlgorithmIdentifier} method
 * @return {Promise<string>}
 */
async function nativeHash(data, method){
    const hashBuffer = await crypto.subtle.digest(method, new TextEncoder().encode(data));
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    return hashArray.map(b => b.toString(16).padStart(2, "0")).join("");
}

async function fallbackHash(data, method) {
    // shajs does not have a dash between algorithm and size
    method = method.replaceAll("-", "")
    return shajs(method).update(data).digest("hex")
}

async function doHash(data, method) {
    // If native hash fails, for e.g. missing availability of crypto.subtle
    // fallback to a js implementation of sha
    try {
        return nativeHash(data, method)
    } catch (_) {
        return fallbackHash(data, method)
    }
}

/**
 * Challenge POW.
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOW(challenge){
    let hash;
    let i;
    // eslint-disable-next-line no-constant-condition
    for (i = 0; true; i++){
        hash = await doHash(challenge.r + i.toString(), "sha-256");
        if (hash.startsWith("0000")){
            break;
        }
    }

    await fetch("/cdn-cgi/challenge-platform/challenge", {
        method: "POST",
        body: challenge.r + "-" + challenge.s + "-" + i.toString(),
    });
}

/**
 * No challenge.
 *
 * @return {Promise<void>}
 */
async function challengeNone(){
    return new Promise((resolve) => {
        setTimeout(resolve, 3000);
    });
}

export function getChallengeSolver(challengeType){
    switch (challengeType){
        case 0:
            return ["Please wait...", challengeNone];
        case 1:
            return ["Solving POW challenge...", challengePOW];
        default:
            throw new Error(`Unknown challenge type: ${challengeType}`);
    }
}
