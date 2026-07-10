init:{
    await document.fonts.ready;
    document.title = "Verifying - Example";
}

challengeStart:{
    console.info("Challenge started", challenge.t);
}

success:{
    document.documentElement.dataset.challengeStatus = "success";
    console.info("Challenge passed", {challenge, countdown});
}

failure:{
    document.documentElement.dataset.challengeStatus = "failure";
    console.error("Challenge failed", {challenge, countdown, result});
}
