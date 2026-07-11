import assert from "node:assert/strict";
import test from "node:test";

import {captchaBlockedAdvice} from "../src/challange/capabilities.js";
import {captchaProviders, challengeCaptcha, getChallengeSolver} from "../src/challange/challanges.js";

function scriptEnvironment(onScript){
    return {
        document: {
            createElement: () => ({}),
            head: {
                append(script){
                    queueMicrotask(() => onScript(script));
                },
            },
        },
    };
}

test("describes every captcha challenge type", () => {
    assert.deepEqual(Object.keys(captchaProviders), ["3", "4", "5"]);

    for (const provider of Object.values(captchaProviders)){
        assert.match(provider.script, /^https:\/\//);
        assert.notEqual(provider.name, "");
        assert.notEqual(provider.global, "");
    }
});

test("solves captcha challenge types with the captcha solver", () => {
    for (const [challengeType, provider] of Object.entries(captchaProviders)){
        const [name, solver] = getChallengeSolver(Number(challengeType));
        assert.equal(solver, challengeCaptcha);
        assert.match(name, new RegExp(provider.name));
    }

    assert.throws(() => getChallengeSolver(99), /Unknown challenge type/);
});

test("advises about content blockers when the provider script fails to load", async() => {
    const environment = scriptEnvironment((script) => script.onerror(new Error("blocked")));

    await assert.rejects(challengeCaptcha({k: "sitekey", t: 3}, {environment}), (error) => {
        assert.match(error.message, /Turnstile/);
        assert.equal(error.advice, captchaBlockedAdvice);
        return true;
    });
});

test("advises about content blockers when the provider API is missing after load", async() => {
    const environment = scriptEnvironment((script) => script.onload());

    await assert.rejects(challengeCaptcha({k: "sitekey", t: 3}, {environment}), (error) => {
        assert.match(error.message, /Turnstile/);
        assert.equal(error.advice, captchaBlockedAdvice);
        return true;
    });
});

test("renders the widget and submits its token", async() => {
    const requests = [];
    const rendered = [];
    const widget = {style: {}};

    const environment = scriptEnvironment((script) => {
        // hcaptcha signals readiness through the named onload callback.
        const callbackName = new URL(script.src).searchParams.get("onload");
        assert.notEqual(callbackName, null);
        environment[callbackName]();
    });
    environment.fetch = async(url, options) => {
        requests.push({options, url});
        return {ok: true};
    };
    environment.hcaptcha = {
        render(container, options){
            rendered.push({container, options});
            queueMicrotask(() => options.callback("widget-response-token"));
        },
    };

    globalThis.document = {
        getElementById: () => widget,
        querySelector: () => ({style: {}}),
    };
    try {
        await challengeCaptcha({k: "sitekey-under-test", t: 4}, {environment});
    }
    finally {
        delete globalThis.document;
    }

    assert.equal(rendered[0].container, widget);
    assert.equal(rendered[0].options.sitekey, "sitekey-under-test");
    assert.equal(requests[0].url, "/cdn-cgi/challenge-platform/challenge");
    assert.equal(requests[0].options.method, "POST");
    assert.equal(requests[0].options.body, "widget-response-token");
});
