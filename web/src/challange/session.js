/**
 * Per-page session / correlation id.
 *
 * Shown to the visitor (as "ena@<uuid>") and attached to challenge requests so
 * support can correlate a report with server logs. It is randomly generated and
 * never derived from the visitor's IP, so it leaks no personal data.
 */

let sessionId = null;

/**
 * @return {string}
 */
function uuid(){
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"){
        return crypto.randomUUID();
    }
    // Fallback for insecure contexts where crypto.randomUUID is unavailable.
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
        const r = Math.floor(Math.random() * 16);
        const v = c === "x" ? r : (r & 0x3) | 0x8;
        return v.toString(16);
    });
}

/**
 * The stable per-page session id, e.g. "ena@3f2b...".
 *
 * @return {string}
 */
export function getSessionId(){
    if (sessionId === null){
        sessionId = `ena@${uuid()}`;
    }
    return sessionId;
}
