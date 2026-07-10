import "./style.scss";
import {doChallenge} from "./challange/challanger";


(() => {
    window.addEventListener("DOMContentLoaded", async() => {
        /* @berghain:inline init */

        if (!navigator.cookieEnabled){
            const cookieWarning = document.getElementById("cookie-warning");
            if (cookieWarning){
                cookieWarning.style.display = "block";
            }
            return;
        }

        await doChallenge();
    });
})();
