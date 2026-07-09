import hooks from "berghain-hooks";
import {getChallengeSolver} from "./challanges";
import {getSessionId} from "./session";
import * as loader from "./loader.js";

/**
 * Decide and solve browser challenges.
 */

// Challenge type served by HAProxy (not Berghain) when the client is banned.
// Carries a full-integer "remaining" seconds field instead of the single-digit "c".
const CHALLENGE_TYPE_BANNED = 3;

/**
 * Get challenge.
 *
 * @return {Promise<object>}
 */
async function getChallenge(){
    const resp = await fetch("/cdn-cgi/challenge-platform/challenge", {
        headers: {
            "X-Berghain-Session": getSessionId(),
        },
    });
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
    let result;
    try {
        loader.setChallengeInfo("Fetching challenge...");

        const challenge = await getChallenge();
        const {t} = challenge;

        if (t === CHALLENGE_TYPE_BANNED){
            hooks.onBanned?.(challenge.remaining);
            loader.banCountdown(challenge.remaining);
            return;
        }

        countdown = challenge.c;

        hooks.onChallengeStart?.(challenge);

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
        hooks.onFailure?.(result);
        loader.showError(result);
    }
    else {
        hooks.onSuccess?.();
    }
}
