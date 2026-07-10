/**
 * Collection of challenges.
 */

import {solvePOWNonce} from "./pow.js";

/**
 * Submit a solved POW nonce.
 *
 * @param {object} challenge
 * @param {number} nonce
 * @return {Promise<void>}
 */
async function submitPOWSolution(challenge, nonce){
    const response = await fetch("/cdn-cgi/challenge-platform/challenge", {
        body: challenge.r + "-" + challenge.s + "-" + nonce.toString(),
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
 * Challenge POW.
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOW(challenge){
    const nonce = await solvePOWNonce(challenge);
    await submitPOWSolution(challenge, nonce);
}

/**
 * Run a POW Worker and return its validated response. The Worker constructor
 * is injected so orchestration and failure handling can be tested in Node.
 *
 * @param {object} challenge
 * @param {new () => Worker} WorkerConstructor
 * @return {Promise<number>}
 */
export async function solvePOWWithWorker(challenge, WorkerConstructor){
    const worker = new WorkerConstructor();

    try {
        return await new Promise((resolve, reject) => {
            worker.onmessage = (event) => {
                const result = event.data;
                if (typeof result?.error === "string"){
                    reject(new Error(`POW worker failed: ${result.error}`));
                    return;
                }
                if (!Number.isSafeInteger(result?.nonce) || result.nonce < 0){
                    reject(new Error("POW worker returned an invalid nonce"));
                    return;
                }
                resolve(result.nonce);
            };
            worker.onerror = (event) => {
                const detail = typeof event?.message === "string" ? `: ${event.message}` : "";
                reject(new Error(`POW worker failed${detail}`));
            };
            worker.onmessageerror = () => reject(new Error("POW worker returned an unreadable message"));
            worker.postMessage(challenge);
        });
    }
    finally {
        worker.terminate();
    }
}

/**
 * Challenge POW inside an inline Web Worker. Vite's Worker module is loaded
 * only when the server explicitly selects challenge type 2.
 *
 * @param {object} challenge
 * @return {Promise<void>}
 */
async function challengePOWWorker(challenge){
    const workerModule = await import("./powWorker.js?worker&inline");
    const PowWorker = workerModule.default;
    const nonce = await solvePOWWithWorker(challenge, PowWorker);
    await submitPOWSolution(challenge, nonce);
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
            return ["Solving POW challenge in a Worker...", challengePOWWorker];
        default:
            throw new Error(`Unknown challenge type: ${challengeType}`);
    }
}
