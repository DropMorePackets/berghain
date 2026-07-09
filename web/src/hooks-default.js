/**
 * Default (no-op) challenge-page hooks.
 *
 * Operators can customise challenge-page behaviour and branding WITHOUT forking
 * web/ by pointing the VITE_HOOKS env var at their own module that default-exports
 * an object implementing any of the functions below. Combine with VITE_ENTRYPOINT
 * (a custom index.html) for full theming from a separate repository.
 *
 * Example custom hooks module:
 *
 *   export default {
 *       onInit({sessionId}){ document.title = "Verifying — Example"; },
 *       onSuccess(){ console.log("passed"); },
 *   };
 *
 * @typedef {object} Hooks
 * @property {(info: {sessionId: string}) => void} [onInit] Page loaded.
 * @property {(missing: {name: string, message: string, fix: string}[]) => void} [onCapabilities] Missing browser capabilities were detected.
 * @property {(challenge: object) => void} [onChallengeStart] A challenge was fetched and is about to be solved.
 * @property {() => void} [onSuccess] The challenge was solved.
 * @property {(error: string) => void} [onFailure] The challenge failed.
 * @property {(remainingSeconds: number) => void} [onBanned] The client is banned.
 */

/** @type {Hooks} */
export default {};
