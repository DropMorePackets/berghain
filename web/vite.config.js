import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";
import { createHtmlPlugin } from "vite-plugin-html";

export default defineConfig({
    css: {
        preprocessorOptions: {
            scss: {
                api: "modern-compiler",
            },
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
        assetsInlineLimit: 100000000,
    },
});
