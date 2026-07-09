/**
 * Collection of challenges.
 */

import PowWorker from "./powWorker.js?worker&inline";
import {doHash, hasLeadingZeroBits} from "./pow";
import {getSessionId} from "./session";

/**
 * Submit a solved POW nonce for a challenge.
 *
 * @param {object} challenge
 * @param {number} nonce
 * @return {Promise<void>}
 */
async function submitSolution(challenge, nonce){
    try {
        const response = await fetch("/cdn-cgi/challenge-platform/challenge", {
            body: challenge.r + "-" + challenge.s + "-" + nonce.toString(),
            headers: {
                "Content-Type": "text/plain",
                "X-Berghain-Session": getSessionId(),
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
 * Challenge POW, solved on the main thread.
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOW(challenge){
    const difficulty = parseInt(challenge.d, 16);
    let i;

    // eslint-disable-next-line no-constant-condition
    for (i = 0; true; i++){
        const hash = await doHash(challenge.r + i.toString());
        if (hasLeadingZeroBits(hash, difficulty)){
            break;
        }
    }

    await submitSolution(challenge, i);
}

/**
 * Challenge POW, solved inside a Web Worker (required by the pow-worker type).
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOWWorker(challenge){
    const difficulty = parseInt(challenge.d, 16);
    const worker = new PowWorker();

    try {
        const nonce = await new Promise((resolve, reject) => {
            worker.onmessage = (e) => resolve(e.data);
            worker.onerror = () => reject(new Error("Web Worker failed"));
            worker.postMessage({r: challenge.r, d: difficulty});
        });
        await submitSolution(challenge, nonce);
    }
    finally {
        worker.terminate();
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
        case 2:
            return ["Solving POW challenge (worker)...", challengePOWWorker];
        default:
            throw new Error(`Unknown challenge type: ${challengeType}`);
    }
}
