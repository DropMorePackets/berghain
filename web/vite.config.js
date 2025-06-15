import {fileURLToPath} from "node:url";
import {dirname, resolve, join} from "node:path";
import {defineConfig, loadEnv} from "vite";
import {viteSingleFile} from "vite-plugin-singlefile";
import {createHtmlPlugin} from "vite-plugin-html";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default defineConfig(({mode}) => {
    const env = loadEnv(mode, process.cwd(), "");

    return {
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
