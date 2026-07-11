/**
 * Collection of challenges.
 */

import {sha256} from "@noble/hashes/sha256";
import {bytesToHex} from "@noble/hashes/utils";
import {captchaBlockedAdvice} from "./capabilities.js";
import * as loader from "./loader.js";

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

export const captchaProviders = Object.freeze({
    3: Object.freeze({
        name: "Turnstile",
        script: "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",
        global: "turnstile",
    }),
    4: Object.freeze({
        name: "hCaptcha",
        script: "https://js.hcaptcha.com/1/api.js?render=explicit",
        global: "hcaptcha",
        // hcaptcha initializes asynchronously after the script has
        // loaded and signals readiness via a named onload callback.
        useOnload: true,
    }),
    5: Object.freeze({
        name: "reCAPTCHA",
        script: "https://www.google.com/recaptcha/api.js?render=explicit",
        global: "grecaptcha",
        // grecaptcha initializes asynchronously after the script has
        // loaded and queues ready() callbacks until then. Turnstile and
        // hCaptcha are usable directly; turnstile.ready() even throws
        // when the script was loaded async.
        useReady: true,
    }),
});

const captchaOnloadCallback = "__berghainCaptchaLoaded";

function loadProviderScript(provider, environment){
    return new Promise((resolve, reject) => {
        const script = environment.document.createElement("script");
        script.src = provider.script;
        if (provider.useOnload){
            environment[captchaOnloadCallback] = () => {
                delete environment[captchaOnloadCallback];
                resolve();
            };
            script.src += `&onload=${captchaOnloadCallback}`;
        }
        else {
            script.onload = () => resolve();
        }
        script.async = true;
        script.onerror = () => reject(new Error(`Failed to load ${script.src}`));
        environment.document.head.append(script);
    });
}

/**
 * Load the widget API of a captcha provider.
 *
 * @param {{name: string, script: string, global: string}} provider
 * @param {object} environment
 * @return {Promise<object>}
 */
async function loadCaptchaApi(provider, environment){
    let api;
    try {
        await loadProviderScript(provider, environment);
        api = environment[provider.global];
    }
    catch {
        api = undefined;
    }

    if (!api){
        const error = new Error(`${provider.name} is unavailable.`);
        error.advice = captchaBlockedAdvice;
        throw error;
    }

    if (provider.useReady){
        await new Promise((resolve) => api.ready(resolve));
    }

    return api;
}

/**
 * Challenge captcha. Renders the provider widget and submits its
 * response token for validation.
 *
 * @param {object} challenge
 * @param {{environment?: object}} [options]
 * @return {Promise<void>}
 */
export async function challengeCaptcha(challenge, {environment = globalThis} = {}){
    const provider = captchaProviders[challenge.t];
    const api = await loadCaptchaApi(provider, environment);

    const container = loader.showWidget();
    let token;
    try {
        token = await new Promise((resolve, reject) => {
            api.render(container, {
                sitekey: challenge.k,
                callback: resolve,
                "error-callback": () => reject(new Error(`${provider.name} reported a widget error`)),
            });
        });
    }
    finally {
        loader.hideWidget();
    }

    const response = await environment.fetch("/cdn-cgi/challenge-platform/challenge", {
        body: token,
        headers: {
            "Content-Type": "text/plain",
        },
        method: "POST",
    });
    if (!response.ok){
        throw new Error("Challenge submission failed");
    }
}

export function getChallengeSolver(challengeType){
    switch (challengeType){
        case 0:
            return ["Please wait...", challengeNone];
        case 1:
            return ["Solving POW challenge...", challengePOW];
        case 3:
        case 4:
        case 5:
            return [`Waiting for ${captchaProviders[challengeType].name}...`, challengeCaptcha];
        default:
            throw new Error(`Unknown challenge type: ${challengeType}`);
    }
}
