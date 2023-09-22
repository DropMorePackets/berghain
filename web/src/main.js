import "./style.scss";
import {doChallenge} from "./challange/challanger";


(() => {
    window.addEventListener("DOMContentLoaded", async() => {
        if (!navigator.cookieEnabled){
            const noCookie = document.getElementById("no-cookie");
            noCookie.style.display = "block";
            return;
        }

        await doChallenge();
    });
})();
