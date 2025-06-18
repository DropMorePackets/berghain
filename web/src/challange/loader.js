/**
 * Countdown loader.
 */

/**
 * Start the countdown.
 */
export function start(){
    const container =  /** @type {HTMLDivElement} */ (document.querySelector(".captcha-container"));
    container.classList.add("alert-warning");
    const loader = /** @type {HTMLDivElement} */ (document.querySelector(".circle-loader"));
    loader.style.visibility = "visible";
}

export function showError(error){
    const errors = /** @type {HTMLDivElement} */ (document.querySelector(".error-container"));
    errors.insertAdjacentHTML("beforeend", `<code>${error}</code>`);
}

export function setChallengeInfo(text){
    const captcha = /** @type {HTMLDivElement} */ (document.querySelector(".captcha"));
    captcha.innerText = text;
}

/**
 * Stop the countdown.
 *
 * @param countdown
 * @param {boolean} [failed=false]
 */
export function stop(countdown, failed = false){
    const loader = /** @type {HTMLDivElement} */ (document.querySelector(".circle-loader"));
    const checkmark = /** @type {HTMLDivElement} */ (document.querySelector(".checkmark"));
    const container =  /** @type {HTMLDivElement} */ (document.querySelector(".captcha-container"));
    const cross = /** @type {HTMLDivElement} */ (document.querySelector(".cross"));

    loader.classList.add("load-complete");
    container.classList.remove("alert-warning");

    failed
        ? cross.style.display = "block"
        : checkmark.style.display = "block";

    if (failed){
        container.classList.add("alert-danger");
        setChallengeInfo("Challenge failed.");
        return;
    }

    setChallengeInfo("Challenge succeeded.");
    container.classList.add("alert-success");

    const interval = setInterval(() => {
        setChallengeInfo(`Reloading in ${countdown}...`);
        if (countdown === 0){
            clearInterval(interval);
            window.location.reload();
        }
        else if (countdown > 0){
            // eslint-disable-next-line no-param-reassign
            countdown--;
        }
    }, 1000);
}
