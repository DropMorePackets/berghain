import {fileURLToPath} from "node:url";
import {dirname, resolve, join} from "node:path";
import {defineConfig, loadEnv} from "vite";
import {viteSingleFile} from "vite-plugin-singlefile";
import {createHtmlPlugin} from "vite-plugin-html";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default defineConfig(({mode}) => {
    const env = loadEnv(mode, process.cwd(), "");

    return {
        resolve: {
            alias: {
                // Operators can override challenge-page behaviour without forking
                // web/ by pointing VITE_HOOKS at their own hooks module.
                "berghain-hooks": env.VITE_HOOKS
                    ? resolve(process.cwd(), env.VITE_HOOKS)
                    : resolve(__dirname, "src/hooks-default.js"),
            },
        },
        plugins: [
            viteSingleFile(),
            createHtmlPlugin({
                minify: true,
            }),
        ],
        build: {
            cssCodeSplit: false,
            assetsInlineLimit: Number.MAX_SAFE_INTEGER, // Inline all assets
            rollupOptions: {
                input: {
                    main: resolve(__dirname, env.VITE_ENTRYPOINT ?? "index.html"),
                },
            },
            emptyOutDir: false,
            outDir: resolve(__dirname, env.VITE_OUTPUT_DIR ?? join("dist", mode)),
        },
    };
});
