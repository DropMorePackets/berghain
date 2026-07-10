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

    if (countdown === 0){
        setChallengeInfo("Reloading ...");
        window.location.reload();
    }

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

/**
 * Format a duration in seconds as "Dd HH:MM:SS" (days omitted when zero).
 *
 * @param {number} totalSeconds
 * @return {string}
 */
export function formatDuration(totalSeconds){
    const days = Math.floor(totalSeconds / 86400);
    const hours = Math.floor((totalSeconds % 86400) / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    const pad = (value) => value.toString().padStart(2, "0");

    if (days > 0){
        return `${days}d ${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
    }
    return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
}

/**
 * Render HAProxy's remaining flat-ban duration and reload when it expires.
 *
 * @param {number} remainingSeconds
 */
export function banCountdown(remainingSeconds){
    const loader = /** @type {HTMLDivElement} */ (document.querySelector(".circle-loader"));
    const container = /** @type {HTMLDivElement} */ (document.querySelector(".captcha-container"));
    if (loader){
        loader.style.visibility = "hidden";
    }
    if (container){
        container.classList.remove("alert-warning");
        container.classList.add("alert-danger");
    }

    let remaining = Math.max(0, Math.floor(Number(remainingSeconds) || 0));
    const render = () => {
        if (remaining <= 0){
            setChallengeInfo("Block expired. Reloading ...");
            window.location.reload();
            return;
        }
        setChallengeInfo(`Too many verification attempts. Try again in ${formatDuration(remaining)}.`);
    };

    render();
    if (remaining <= 0){
        return;
    }

    const interval = setInterval(() => {
        remaining--;
        if (remaining <= 0){
            clearInterval(interval);
        }
        render();
    }, 1000);
}
