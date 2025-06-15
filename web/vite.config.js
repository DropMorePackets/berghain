import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { defineConfig, loadEnv } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";
import { createHtmlPlugin } from "vite-plugin-html";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default defineConfig(({ mode }) => {
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
            assetsInlineLimit: 100000000,
            rollupOptions: {
                input: {
                    main: resolve(__dirname, env.VITE_ENTRYPOINT ?? "index.html"),
                },
            },
        },
    };
});
