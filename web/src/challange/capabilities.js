/**
 * Advice shown when a captcha provider script cannot be loaded, the
 * most common cause being a content blocker. Unlike the capabilities
 * below this can only be detected once loading the script has failed.
 */
export const captchaBlockedAdvice = Object.freeze({
    name: "Captcha provider",
    message: "The captcha provider script could not be loaded.",
    fix: "Disable content blockers for this page, check your network connection, and reload.",
});

const advice = {
    textEncoder: {
        name: "Text encoding",
        message: "This challenge needs the browser TextEncoder API.",
        fix: "Use an up-to-date browser with JavaScript enabled.",
    },
    webCrypto: {
        name: "Web Crypto",
        message: "This challenge needs the Web Crypto API.",
        fix: "Use an up-to-date browser over HTTPS and disable extensions that block crypto.subtle.",
    },
    worker: {
        name: "Web Workers",
        message: "This challenge must run in a Web Worker.",
        fix: "Disable extensions that block Web Workers or try a standard browser configuration.",
    },
};

/**
 * Return advice only for missing capabilities required by this challenge.
 *
 * @param {number} challengeType
 * @param {{environment?: object, nativeCrypto?: boolean}} [options]
 * @return {{name: string, message: string, fix: string}[]}
 */
export function detectMissingCapabilities(challengeType, {
    environment = globalThis,
    nativeCrypto = import.meta.env.VITE_NATIVE_CRYPTO === "true",
} = {}){
    if (challengeType !== 1 && challengeType !== 2){
        return [];
    }

    const missing = [];
    if (typeof environment.TextEncoder !== "function"){
        missing.push(advice.textEncoder);
    }
    if (nativeCrypto && !environment.crypto?.subtle){
        missing.push(advice.webCrypto);
    }
    if (challengeType === 2 && typeof environment.Worker !== "function"){
        missing.push(advice.worker);
    }
    return missing;
}
