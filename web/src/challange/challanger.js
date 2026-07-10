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
    const resp = await fetch("/cdn-cgi/challenge-platform/challenge");
    return await resp.json();
}

/**
 * Do challenge.
 *
 * @return {Promise<void>}
 */
export async function doChallenge(){
    loader.start();

    let countdown = 3;
    let challenge;
    let result;
    try {
        loader.setChallengeInfo("Fetching challenge...");

        challenge = await getChallenge();
        const {t} = challenge;
        countdown = challenge.c;

        /* @berghain:inline challenge-start */

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

    if (result){
        /* @berghain:inline failure */
    }
    else {
        /* @berghain:inline success */
    }

    loader.stop(countdown, !!result);
    if (result){
        loader.showError(result);
    }
}
