import assert from "node:assert/strict";
import {mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {dirname, join, relative, resolve, sep} from "node:path";
import {fileURLToPath} from "node:url";
import {test} from "node:test";
import {build as viteBuild} from "vite";
import {createInlineHooksPlugin} from "./inline-hooks.js";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainFilename = resolve(root, "src/main.js");
const challengerFilename = resolve(root, "src/challange/challanger.js");

async function createHooksFile(testContext, code, supportingFiles = {}){
    const hooksDirectory = await mkdtemp(join(tmpdir(), "berghain-inline-hooks-"));
    const hooksFile = resolve(hooksDirectory, "hooks.js");
    testContext.after(async() => await rm(hooksDirectory, {force: true, recursive: true}));

    await writeFile(hooksFile, code);
    await Promise.all(Object.entries(supportingFiles).map(async([filename, contents]) => {
        await writeFile(resolve(hooksDirectory, filename), contents);
    }));
    return hooksFile;
}

function createPluginContext(){
    const watchedFiles = [];
    return {
        addWatchFile(filename){
            watchedFiles.push(filename);
        },
        error(error){
            throw error;
        },
        watchedFiles,
    };
}

async function transform(plugin, code, id){
    const context = createPluginContext();
    const result = await plugin.transform.call(context, code, id);
    return {context, result};
}

function expectedRelativeImport(targetFilename, importedFilename){
    let importPath = relative(dirname(targetFilename), importedFilename).split(sep).join("/");
    if (!importPath.startsWith(".")){
        importPath = `./${importPath}`;
    }
    return importPath;
}

test("removes unconfigured inline phases without leaving runtime hooks", async() => {
    const plugin = createInlineHooksPlugin({hooksFile: null, root});
    const mainSource = await readFile(mainFilename, "utf8");
    const challengerSource = await readFile(challengerFilename, "utf8");

    const {result: mainResult} = await transform(plugin, mainSource, mainFilename);
    const {result: challengerResult} = await transform(plugin, challengerSource, challengerFilename);

    for (const result of [mainResult, challengerResult]){
        assert.doesNotMatch(result.code, /@berghain:inline|__BERGHAIN_INLINE_PHASE__|createHookInvoker/);
        assert.ok(result.map);
    }
    assert.equal(await plugin.transform.call(createPluginContext(), "const untouched = true;", resolve(root, "src/other.js")), null);
});

test("inlines labeled, awaited phase blocks with locals and source maps", async(testContext) => {
    const hooksSource = [
        "init: {",
        "    const phaseValue = await Promise.resolve(\"ready\");",
        "    globalThis.inlineInit = phaseValue;",
        "}",
        "challengeStart: {",
        "    globalThis.inlineChallenge = challenge.t;",
        "}",
        "failure: {",
        "    globalThis.inlineFailure = {challenge, countdown, result};",
        "    {",
        "        const result = \"block scoped\";",
        "        globalThis.inlineLocalResult = result;",
        "    }",
        "}",
        "success: {",
        "    globalThis.inlineSuccess = {challenge, countdown};",
        "}",
        "",
    ].join("\n");
    const hooksFile = await createHooksFile(testContext, hooksSource);
    const plugin = createInlineHooksPlugin({hooksFile, root});
    const mainSource = await readFile(mainFilename, "utf8");
    const challengerSource = await readFile(challengerFilename, "utf8");

    const {context: mainContext, result: mainResult} = await transform(plugin, mainSource, mainFilename);
    const {result: challengerResult} = await transform(plugin, challengerSource, challengerFilename);

    assert.ok(mainResult.code.indexOf("inlineInit") < mainResult.code.indexOf("navigator.cookieEnabled"));
    assert.ok(challengerResult.code.indexOf("inlineChallenge") < challengerResult.code.lastIndexOf("getChallengeSolver"));
    assert.ok(challengerResult.code.indexOf("inlineFailure") < challengerResult.code.indexOf("loader.stop"));
    assert.ok(challengerResult.code.indexOf("inlineSuccess") < challengerResult.code.indexOf("loader.stop"));
    assert.match(mainResult.code, /await Promise\.resolve\(["']ready["']\)/);
    assert.match(challengerResult.code, /const result = ["']block scoped["']/);
    assert.doesNotMatch(mainResult.code, /\btry\b|\bcatch\b|Berghain inline phase/);
    assert.ok(mainContext.watchedFiles.includes(hooksFile));
    assert.ok(mainResult.map.sources.includes(hooksFile));
    assert.equal(mainResult.map.sourcesContent[mainResult.map.sources.indexOf(hooksFile)], hooksSource);
});

test("rebases shared imports and renames bindings that collide with the target", async(testContext) => {
    const hooksFile = await createHooksFile(testContext, [
        "import loader from \"./helper.js\";",
        "challengeStart: {",
        "    globalThis.inlineImported = loader(challenge);",
        "}",
        "",
    ].join("\n"), {
        "helper.js": "export default (challenge) => challenge.t;\n",
    });
    const plugin = createInlineHooksPlugin({hooksFile, root});
    const challengerSource = await readFile(challengerFilename, "utf8");

    const {result} = await transform(plugin, challengerSource, challengerFilename);

    const helperFilename = resolve(dirname(hooksFile), "helper.js");
    const escapedImport = expectedRelativeImport(challengerFilename, helperFilename).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    assert.match(result.code, new RegExp(escapedImport));
    assert.match(result.code, /import .* as loader from ["']\.\/loader\.js["']/);
    assert.doesNotMatch(result.code, /import loader from/);
    assert.match(result.code, /inlineImported/);
});

test("rejects old callback modules and invalid phase files", async(testContext) => {
    const hooksDirectory = await mkdtemp(join(tmpdir(), "berghain-inline-hooks-directory-"));
    testContext.after(async() => await rm(hooksDirectory, {force: true, recursive: true}));
    const directoryPlugin = createInlineHooksPlugin({hooksFile: hooksDirectory, root});
    const mainSource = await readFile(mainFilename, "utf8");
    await assert.rejects(transform(directoryPlugin, mainSource, mainFilename), /must point to a JavaScript file/);

    const callbackFile = await createHooksFile(testContext, "export default {onInit(){}};\n");
    const callbackPlugin = createInlineHooksPlugin({hooksFile: callbackFile, root});
    await assert.rejects(transform(callbackPlugin, mainSource, mainFilename), /top level may only contain/);

    const unknownFile = await createHooksFile(testContext, "unknown: { console.log(\"never\"); }\n");
    const unknownPlugin = createInlineHooksPlugin({hooksFile: unknownFile, root});
    await assert.rejects(transform(unknownPlugin, mainSource, mainFilename), /unknown phase label "unknown"/);

    const duplicateFile = await createHooksFile(testContext, "init: {}\ninit: {}\n");
    const duplicatePlugin = createInlineHooksPlugin({hooksFile: duplicateFile, root});
    await assert.rejects(transform(duplicatePlugin, mainSource, mainFilename), /phase "init" is declared more than once/);

    const unsupportedFile = await createHooksFile(testContext, "init: { var phaseValue = true; }\n");
    const unsupportedPlugin = createInlineHooksPlugin({hooksFile: unsupportedFile, root});
    await assert.rejects(transform(unsupportedPlugin, mainSource, mainFilename), /cannot use var/);

    const escapingFile = await createHooksFile(testContext, "init: { break init; }\n");
    const escapingPlugin = createInlineHooksPlugin({hooksFile: escapingFile, root});
    await assert.rejects(transform(escapingPlugin, mainSource, mainFilename), /cannot break or continue/);
});

test("rejects missing, duplicate, and malformed source markers", async() => {
    const plugin = createInlineHooksPlugin({hooksFile: null, root});
    const mainSource = await readFile(mainFilename, "utf8");
    const marker = "        /* @berghain:inline init */";

    await assert.rejects(transform(plugin, mainSource.replace(marker, ""), mainFilename), /Missing inline phase "init"/);
    await assert.rejects(transform(plugin, mainSource.replace(marker, `${marker}\n${marker}`), mainFilename), /Duplicate inline phase "init"/);
    await assert.rejects(transform(plugin, mainSource.replace(marker, "        /* @berghain:inline */"), mainFilename), /Malformed inline phase marker/);
});

test("Vite builds the repository example without runtime hook machinery", async() => {
    const hooksFile = resolve(root, "examples/challenge-page.js");
    const buildResult = await viteBuild({
        build: {
            minify: false,
            rollupOptions: {
                input: resolve(root, "index.html"),
            },
            write: false,
        },
        configFile: false,
        logLevel: "silent",
        plugins: [createInlineHooksPlugin({hooksFile, root})],
        root,
    });
    const outputs = Array.isArray(buildResult) ? buildResult : [buildResult];
    const bundledCode = outputs.flatMap(({output}) => output)
        .filter(({type}) => type === "chunk")
        .map(({code}) => code)
        .join("\n");

    assert.match(bundledCode, /Verifying - Example/);
    assert.match(bundledCode, /challengeStatus/);
    assert.doesNotMatch(bundledCode, /@berghain:inline|createHookInvoker|berghain-hooks|Berghain (?:hook|inline phase)/);
});
