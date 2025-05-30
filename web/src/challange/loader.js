/**
 * Countdown loader.
 */

/**
 * Start the countdown.
 */
export function start(){
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
 * @param {boolean} [failed=false]
 */
export function stop(countdown, failed = false){
    const loader = /** @type {HTMLDivElement} */ (document.querySelector(".circle-loader"));
    const checkmark = /** @type {HTMLDivElement} */ (document.querySelector(".checkmark"));
    const cross = /** @type {HTMLDivElement} */ (document.querySelector(".cross"));

    loader.classList.add("load-complete");

    failed
        ? cross.style.display = "block"
        : checkmark.style.display = "block";

    if (failed){
        return;
    }

    const interval = setInterval(() => {
        setChallengeInfo(`Reloading in ${countdown}...`);
        if (countdown === 0){
            clearInterval(interval);
            window.location.reload();
        }
        else if (countdown > 0) {
            countdown--;
        }
    }, 1000);
}
