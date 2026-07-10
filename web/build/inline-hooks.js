import {readFile, stat} from "node:fs/promises";
import {dirname, isAbsolute, relative, resolve, sep} from "node:path";
import babel from "@babel/core";
import {searchForWorkspaceRoot} from "vite";

const {parseAsync, transformFromAstAsync, traverse, types} = babel;

const MARKER_TOKEN = "@berghain:inline";
const SENTINEL = "__BERGHAIN_INLINE_PHASE__";

const PHASE_LABELS = Object.freeze({
    init: "init",
    challengeStart: "challenge-start",
    success: "success",
    failure: "failure",
});

const TARGET_PHASES = Object.freeze({
    "src/main.js": ["init"],
    "src/challange/challanger.js": ["challenge-start", "failure", "success"],
});

function cleanId(id){
    return id.split("?", 1)[0];
}

function isMissingFile(error){
    return error?.code === "ENOENT";
}

function createHooksError(filename, message){
    return new Error(`Invalid VITE_HOOKS file ${filename}: ${message}`);
}

function createPhaseError(phase, filename, message){
    return createHooksError(filename, `phase "${phase}" ${message}`);
}

function getTargetFiles(root){
    return new Map(Object.entries(TARGET_PHASES).map(([filename, phases]) => [
        resolve(root, filename),
        phases,
    ]));
}

function isHooksFile(filename, hooksFile){
    return hooksFile !== null && resolve(filename) === hooksFile;
}

async function validateHooksFile(hooksFile){
    if (!hooksFile){
        return;
    }

    let fileStats;
    try {
        fileStats = await stat(hooksFile);
    }
    catch (error){
        if (isMissingFile(error)){
            throw new Error(`VITE_HOOKS file does not exist: ${hooksFile}`);
        }
        throw error;
    }

    if (!fileStats.isFile()){
        throw new Error(`VITE_HOOKS must point to a JavaScript file: ${hooksFile}`);
    }
}

function prepareMarkers(code, filename, expectedPhases){
    const foundPhases = [];
    const markerPattern = /^([\t ]*)\/\*[\t ]*@berghain:inline[\t ]+([a-z][a-z0-9-]*)[\t ]*\*\/[\t ]*$/gm;
    const preparedCode = code.replace(markerPattern, (marker, indentation, phase) => {
        foundPhases.push(phase);
        return `${indentation}${SENTINEL}("${phase}");`;
    });
    const markerCount = code.split(MARKER_TOKEN).length - 1;

    if (markerCount !== foundPhases.length){
        throw new Error(`Malformed inline phase marker in ${filename}`);
    }

    for (const phase of foundPhases){
        if (!expectedPhases.includes(phase)){
            throw new Error(`Unknown inline phase "${phase}" in ${filename}`);
        }
        if (foundPhases.indexOf(phase) !== foundPhases.lastIndexOf(phase)){
            throw new Error(`Duplicate inline phase "${phase}" in ${filename}`);
        }
    }

    for (const phase of expectedPhases){
        if (!foundPhases.includes(phase)){
            throw new Error(`Missing inline phase "${phase}" in ${filename}`);
        }
    }

    return preparedCode;
}

async function parseModule(code, filename){
    return await parseAsync(code, {
        babelrc: false,
        configFile: false,
        filename,
        parserOpts: {
            sourceFilename: filename,
        },
        sourceType: "module",
    });
}

function getProgramPath(moduleAst){
    let programPath;
    traverse(moduleAst, {
        Program(path){
            programPath = path;
            path.stop();
        },
    });
    return programPath;
}

function getTopLevelPhase(path){
    const phasePath = path.findParent((parentPath) => (
        parentPath.isLabeledStatement()
        && parentPath.parentPath.isProgram()
    ));
    if (!phasePath){
        return null;
    }
    return PHASE_LABELS[phasePath.node.label.name] ?? null;
}

function extractPhases(hooksAst, hooksFile){
    if (hooksAst.program.directives.length > 0){
        throw createHooksError(hooksFile, "module directives are not supported");
    }
    if (hooksAst.program.interpreter){
        throw createHooksError(hooksFile, "hashbang directives are not supported");
    }

    const phases = new Map();
    const imports = [];
    const topLevelAwait = new Set();

    for (const statement of hooksAst.program.body){
        if (types.isImportDeclaration(statement)){
            imports.push(statement);
            continue;
        }
        if (types.isEmptyStatement(statement)){
            continue;
        }
        if (!types.isLabeledStatement(statement)){
            throw createHooksError(
                hooksFile,
                "top level may only contain static imports and init, challengeStart, success, or failure blocks",
            );
        }

        const label = statement.label.name;
        const phase = PHASE_LABELS[label];
        if (!phase){
            throw createHooksError(hooksFile, `unknown phase label "${label}"`);
        }
        if (!types.isBlockStatement(statement.body)){
            throw createPhaseError(phase, hooksFile, "must use a block");
        }
        if (phases.has(phase)){
            throw createPhaseError(phase, hooksFile, "is declared more than once");
        }
        phases.set(phase, statement.body.body);
    }

    traverse(hooksAst, {
        enter(path){
            const phase = getTopLevelPhase(path);
            if (path.isVariableDeclaration({kind: "var"}) && path.getFunctionParent() === null){
                throw createPhaseError(phase, hooksFile, "cannot use var outside a nested function; use let or const");
            }
            if (
                (path.isAwaitExpression() || (path.isForOfStatement() && path.node.await))
                && path.getFunctionParent() === null
                && phase
            ){
                topLevelAwait.add(phase);
            }
            if (path.isMetaProperty() && path.node.meta.name === "import"){
                throw createPhaseError(phase, hooksFile, "cannot use import.meta");
            }
            if (path.node.type === "Import" || path.node.type === "ImportExpression"){
                throw createPhaseError(phase, hooksFile, "cannot use dynamic imports; use a static import");
            }
            if (path.isReferencedIdentifier({name: "arguments"}) && path.getFunctionParent() === null){
                throw createPhaseError(phase, hooksFile, "cannot use top-level arguments");
            }
            if ((path.isBreakStatement() || path.isContinueStatement()) && path.node.label?.name in PHASE_LABELS){
                throw createPhaseError(phase, hooksFile, "cannot break or continue a phase label");
            }
        },
    });

    return {imports, phases, topLevelAwait};
}

function rebaseRelativeImport(importNode, hooksFile, targetFilename){
    const source = importNode.source.value;
    if (!source.startsWith(".")){
        return;
    }

    const suffixIndex = source.search(/[?#]/);
    const sourcePath = suffixIndex === -1 ? source : source.slice(0, suffixIndex);
    const suffix = suffixIndex === -1 ? "" : source.slice(suffixIndex);
    const absoluteImport = resolve(dirname(hooksFile), sourcePath);
    let rebasedImport = relative(dirname(targetFilename), absoluteImport).split(sep).join("/");

    if (!rebasedImport.startsWith(".")){
        rebasedImport = `./${rebasedImport}`;
    }

    importNode.source.value = `${rebasedImport}${suffix}`;
    delete importNode.source.extra;
}

function prepareImports(hooksAst, imports, hooksFile, targetFilename, targetProgramPath){
    const hooksProgramPath = getProgramPath(hooksAst);
    for (const importNode of imports){
        for (const specifier of importNode.specifiers){
            const localName = specifier.local.name;
            const uniqueName = targetProgramPath.scope.generateUidIdentifier(`berghain_${localName}`).name;
            hooksProgramPath.scope.rename(localName, uniqueName);
        }
        rebaseRelativeImport(importNode, hooksFile, targetFilename);
    }
}

function createPhaseBlock(statements){
    return types.blockStatement(statements);
}

function addImports(program, imports){
    let insertionIndex = 0;
    while (insertionIndex < program.body.length && types.isImportDeclaration(program.body[insertionIndex])){
        insertionIndex++;
    }
    program.body.splice(insertionIndex, 0, ...imports);
}

function populateSourcesContent(sourceMap, sourceByFilename, targetFilename){
    if (!sourceMap){
        return;
    }

    sourceMap.sourcesContent = sourceMap.sources.map((source, index) => {
        const absoluteSource = isAbsolute(source) ? source : resolve(dirname(targetFilename), source);
        const originalContent = sourceByFilename.get(absoluteSource);
        if (originalContent !== undefined){
            return originalContent;
        }
        return sourceMap.sourcesContent?.[index] ?? null;
    });
}

async function readHooks(hooksFile){
    if (!hooksFile){
        return null;
    }

    const code = await readFile(hooksFile, "utf8");
    let hooksAst;
    try {
        hooksAst = await parseModule(code, hooksFile);
    }
    catch (error){
        throw createHooksError(hooksFile, error.message);
    }

    return {
        code,
        hooksAst,
        ...extractPhases(hooksAst, hooksFile),
    };
}

function invalidateTargets(server, targetFiles){
    const seen = new Set();
    const timestamp = Date.now();

    for (const targetFilename of targetFiles.keys()){
        const modules = server.moduleGraph.getModulesByFile(targetFilename);
        if (!modules){
            continue;
        }
        for (const moduleNode of modules){
            server.moduleGraph.invalidateModule(moduleNode, seen, timestamp, true);
        }
    }

    server.ws.send({path: "*", type: "full-reload"});
}

/**
 * Inline operator-owned labeled phase blocks into the challenge flow at build time.
 *
 * @param {object} options
 * @param {string} options.root Project root.
 * @param {string|null|undefined} options.hooksFile File containing optional labeled phase blocks.
 * @return {import("vite").Plugin}
 */
export function createInlineHooksPlugin({root, hooksFile}){
    const absoluteRoot = resolve(root);
    const absoluteHooksFile = hooksFile ? resolve(hooksFile) : null;
    const targetFiles = getTargetFiles(absoluteRoot);

    return {
        name: "berghain-inline-hooks",
        enforce: "pre",

        config(){
            if (!absoluteHooksFile){
                return null;
            }
            return {
                server: {
                    fs: {
                        allow: [searchForWorkspaceRoot(absoluteRoot), dirname(absoluteHooksFile)],
                    },
                },
            };
        },

        async buildStart(){
            await validateHooksFile(absoluteHooksFile);
            if (absoluteHooksFile){
                this.addWatchFile(absoluteHooksFile);
            }
        },

        configureServer(server){
            if (!absoluteHooksFile){
                return;
            }

            server.watcher.add(absoluteHooksFile);
            const reloadAddedOrRemovedFile = (filename) => {
                if (isHooksFile(filename, absoluteHooksFile)){
                    invalidateTargets(server, targetFiles);
                }
            };
            server.watcher.on("add", reloadAddedOrRemovedFile);
            server.watcher.on("unlink", reloadAddedOrRemovedFile);
        },

        handleHotUpdate({file, server}){
            if (!isHooksFile(file, absoluteHooksFile)){
                return undefined;
            }

            invalidateTargets(server, targetFiles);
            return [];
        },

        async transform(code, id){
            const targetFilename = resolve(cleanId(id));
            const expectedPhases = targetFiles.get(targetFilename);
            if (!expectedPhases){
                return null;
            }

            try {
                await validateHooksFile(absoluteHooksFile);
                if (absoluteHooksFile){
                    this.addWatchFile(absoluteHooksFile);
                }

                const preparedCode = prepareMarkers(code, targetFilename, expectedPhases);
                const targetAst = await parseModule(preparedCode, targetFilename);
                const targetProgramPath = getProgramPath(targetAst);
                const hooks = await readHooks(absoluteHooksFile);
                const sourceByFilename = new Map([[targetFilename, code]]);
                const phases = new Map();
                let imports = [];

                if (hooks){
                    const {imports: hooksImports} = hooks;
                    sourceByFilename.set(absoluteHooksFile, hooks.code);
                    const hasTargetPhase = expectedPhases.some((phase) => hooks.phases.has(phase));
                    if (hasTargetPhase){
                        prepareImports(
                            hooks.hooksAst,
                            hooksImports,
                            absoluteHooksFile,
                            targetFilename,
                            targetProgramPath,
                        );
                        imports = hooksImports;
                    }

                    for (const phase of expectedPhases){
                        phases.set(phase, {
                            hasTopLevelAwait: hooks.topLevelAwait.has(phase),
                            statements: hooks.phases.get(phase) ?? [],
                        });
                    }
                }

                traverse(targetAst, {
                    ExpressionStatement(path){
                        const {expression} = path.node;
                        if (!types.isCallExpression(expression) || !types.isIdentifier(expression.callee, {name: SENTINEL})){
                            return;
                        }

                        const [phaseNode] = expression.arguments;
                        const phase = types.isStringLiteral(phaseNode) ? phaseNode.value : null;
                        const phaseCode = phases.get(phase);
                        if (!phaseCode || phaseCode.statements.length === 0){
                            path.remove();
                            return;
                        }

                        if (phaseCode.hasTopLevelAwait && !path.getFunctionParent()?.node.async){
                            throw createPhaseError(phase, absoluteHooksFile, "uses await at a non-async injection point");
                        }

                        path.replaceWith(createPhaseBlock(phaseCode.statements));
                    },
                });

                addImports(targetAst.program, imports);
                const transformed = await transformFromAstAsync(targetAst, preparedCode, {
                    babelrc: false,
                    cloneInputAst: false,
                    configFile: false,
                    filename: targetFilename,
                    sourceFileName: targetFilename,
                    sourceMaps: true,
                });

                populateSourcesContent(transformed.map, sourceByFilename, targetFilename);
                return {
                    code: transformed.code,
                    map: transformed.map,
                };
            }
            catch (error){
                return this.error(error);
            }
        },
    };
}
