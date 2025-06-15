/**
 * Collection of challenges.
 */

import {sha256} from "@noble/hashes/sha256";
import {bytesToHex} from "@noble/hashes/utils";

async function doHash(data){
    const input = new TextEncoder().encode(data);

    if (import.meta.env.VITE_NATIVE_CRYPTO === "true"){
        const hashBuffer = await crypto.subtle.digest("sha-256", input);
        return bytesToHex(new Uint8Array(hashBuffer));
    }
    return bytesToHex(sha256(input));
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
        hash = await doHash(challenge.r + i.toString());
        if (hash.startsWith("0000")){
            break;
        }
    }

    try {
        const response = await fetch("/cdn-cgi/challenge-platform/challenge", {
            body: challenge.r + "-" + challenge.s + "-" + i.toString(),
            headers: {
                "Content-Type": "text/plain",
            },
            method: "POST",
        });
        if (!response.ok){
            throw new Error("Challenge submission failed");
        }
    }
    catch (error){
        console.error(error.message);
    }
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
