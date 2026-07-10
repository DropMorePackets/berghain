import "./style.scss";
import {doChallenge} from "./challange/challanger";


(() => {
    window.addEventListener("DOMContentLoaded", async() => {
        const session = document.getElementById("session-id");
        if (!navigator.cookieEnabled){
            const noCookie = document.getElementById("no-cookie");
            noCookie.style.display = "block";
            return;
        }

        const supportId = await doChallenge();
        if (session){
            session.textContent = supportId ?? "";
        }
    });
})();
