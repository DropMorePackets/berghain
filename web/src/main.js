import "./style.scss";
import {doChallenge} from "./challange/challanger";


(() => {
    window.addEventListener("DOMContentLoaded", async() => {
        /* @berghain:inline init */

        const session = document.getElementById("session-id");
        if (!navigator.cookieEnabled){
            const cookieWarning = document.getElementById("cookie-warning");
            if (cookieWarning){
                cookieWarning.style.display = "block";
            }
            return;
        }

        const supportId = await doChallenge();
        if (session){
            session.textContent = supportId ?? "";
        }
    });
})();
