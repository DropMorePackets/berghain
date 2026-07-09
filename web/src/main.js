import "./style.scss";
import hooks from "berghain-hooks";
import {doChallenge} from "./challange/challanger";
import {detectCapabilities} from "./challange/capabilities";
import {getSessionId} from "./challange/session";
import {showCapabilities} from "./challange/loader";


(() => {
    window.addEventListener("DOMContentLoaded", async() => {
        const sessionEl = document.getElementById("session-id");
        if (sessionEl){
            sessionEl.textContent = getSessionId();
        }

        hooks.onInit?.({sessionId: getSessionId()});

        const missing = detectCapabilities().filter((c) => !c.ok);
        showCapabilities(missing);
        if (missing.length){
            hooks.onCapabilities?.(missing);
        }

        if (!navigator.cookieEnabled){
            const cookieWarning = document.getElementById("cookie-warning");
            cookieWarning.style.display = "block";
            return;
        }

        await doChallenge();
    });
})();
