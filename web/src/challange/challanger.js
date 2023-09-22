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

    let result;
    try {
        loader.setChallengeInfo("Fetching challenge...");

        const challenge = await getChallenge();
        const {t} = challenge;

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

    loader.stop(!!result);
    if (result){
        loader.showError(result);
    }
}
