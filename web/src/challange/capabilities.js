/**
 * Browser capability detection.
 *
 * Reports which browser features that Berghain relies on (now or for harder
 * challenges) are unavailable, so the visitor can be told exactly what is
 * missing and how to resolve it.
 */

/**
 * @return {boolean}
 */
function hasCanvas(){
    try {
        return !!document.createElement("canvas").getContext("2d");
    }
    catch{
        return false;
    }
}

/**
 * @return {boolean}
 */
function hasWebGL(){
    try {
        const canvas = document.createElement("canvas");
        return !!(canvas.getContext("webgl") || canvas.getContext("webgl2") || canvas.getContext("experimental-webgl"));
    }
    catch{
        return false;
    }
}

/**
 * Detect browser capabilities relevant to Berghain's challenges.
 *
 * @return {{name: string, ok: boolean, message: string, fix: string}[]}
 */
export function detectCapabilities(){
    // crypto.subtle is only exposed in secure contexts, so it is only required
    // by the native-crypto build, which is served over HTTPS.
    const nativeCrypto = import.meta.env.VITE_NATIVE_CRYPTO === "true";

    return [
        {
            name: "Web Crypto",
            ok: !nativeCrypto || (typeof crypto !== "undefined" && !!crypto.subtle),
            message: "The Web Crypto API (crypto.subtle) is required over HTTPS.",
            fix: "Use a modern browser over HTTPS and disable extensions that block crypto.subtle.",
        },
        {
            name: "Web Workers",
            ok: typeof Worker !== "undefined",
            message: "Web Workers may be required for harder challenges.",
            fix: "Disable extensions that block Web Workers (for example JShelter).",
        },
        {
            name: "WebAssembly",
            ok: typeof WebAssembly !== "undefined",
            message: "WebAssembly may be required for harder challenges.",
            fix: "Use a browser with WebAssembly enabled.",
        },
        {
            name: "Canvas",
            ok: hasCanvas(),
            message: "The Canvas API may be required for harder challenges.",
            fix: "Disable Canvas-blocking extensions (for example Canvas Blocker).",
        },
        {
            name: "WebGL",
            ok: hasWebGL(),
            message: "WebGL may be required for harder challenges.",
            fix: "Enable hardware acceleration / WebGL in your browser settings.",
        },
        {
            name: "RTCPeerConnection",
            ok: typeof RTCPeerConnection !== "undefined",
            message: "RTCPeerConnection may be required for harder challenges.",
            fix: "Enable WebRTC in your browser settings.",
        },
    ];
}
