/**
 * Collection of challenges.
 */

import {doHash, hasLeadingZeroBits, parsePOWDifficulty, powInput} from "./pow.js";

/**
 * Challenge POW.
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOW(challenge){
    const difficulty = parsePOWDifficulty(challenge.d);
    let i;

    // eslint-disable-next-line no-constant-condition
    for (i = 0; true; i++){
        const hash = await doHash(powInput(challenge, i));
        if (hasLeadingZeroBits(hash, difficulty)){
            break;
        }
    }

    const response = await fetch("/cdn-cgi/challenge-platform/challenge", {
        body: challenge.r + "-" + challenge.s + "-" + i.toString(),
        headers: {
            "Content-Type": "text/plain",
        },
        method: "POST",
    });
    if (!response.ok){
        throw new Error(`Challenge submission failed (${response.status})`);
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
