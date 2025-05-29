import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";
import { createHtmlPlugin } from "vite-plugin-html";

export default defineConfig({
    plugins: [
        viteSingleFile(),
        createHtmlPlugin({
            minify: true,
        }),
    ],
    build: {
        cssCodeSplit: false,
        assetsInlineLimit: 100000000,
    },
});
