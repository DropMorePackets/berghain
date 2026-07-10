import {getChallengeSolver} from "./challanges";
import * as loader from "./loader.js";

/**
 * Decide and solve browser challenges.
 */

/**
 * Get challenge.
 *
 * @return {Promise<object>}
 */
async function getChallenge(){
    const response = await fetch("/cdn-cgi/challenge-platform/challenge");
    return response.json();
}

/**
 * Do challenge.
 *
 * @return {Promise<string|null>}
 */
export async function doChallenge(){
    loader.start();

    let countdown = 3;
    let result;
    let session = null;
    try {
        loader.setChallengeInfo("Fetching challenge...");

        const challenge = await getChallenge();
        session = challenge.i ?? null;
        const {t} = challenge;
        countdown = challenge.c;

        const [name, solver] = getChallengeSolver(t);

        loader.setChallengeInfo(name);
        result = await solver(challenge);
        if (!!result){
            result = `Validation result: ${result}`;
        }
    }
    catch (e){
        result = e.toString();
    }

    loader.stop(countdown, !!result);
    if (result){
        loader.showError(result);
    }
    return session;
}
