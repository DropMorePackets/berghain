import globals from "globals";
import babelParser from "@babel/eslint-parser";

export default [{
    files: ["**/*.js"],
    ignores: ["**/dist", "**/node_modules"],
}, {
    plugins: {},

    languageOptions: {
        globals: {
            ...globals.browser,
            ...globals.node,
            ...globals.commonjs,
        },

        parser: babelParser,
        ecmaVersion: "latest",
        sourceType: "module",

        parserOptions: {
            requireConfigFile: false,

            babelOptions: {
                plugins: [],
            },
        },
    },

    rules: {
        strict: [2, "global"],
        "no-var": 2,
        "prefer-const": 2,

        "prefer-destructuring": [2, {
            array: false,
            object: true,
        }, {
            enforceForRenamedProperties: false,
        }],

        "no-shadow": 2,
        "no-shadow-restricted-names": 2,

        "no-unused-vars": [2, {
            vars: "local",
            args: "after-used",
        }],

        "no-use-before-define": 2,
        "comma-dangle": [2, "always-multiline"],
        "no-cond-assign": [2, "always"],
        "no-console": 0,
        "no-debugger": 1,
        "no-alert": 1,
        "no-constant-condition": 1,
        "no-const-assign": 2,
        "no-dupe-keys": 2,
        "no-duplicate-case": 2,
        "no-empty": 2,
        "no-ex-assign": 2,
        "no-extra-boolean-cast": 0,
        "no-extra-semi": 2,
        "no-func-assign": 2,
        "no-inner-declarations": 2,
        "no-invalid-regexp": 2,
        "no-irregular-whitespace": 2,
        "no-obj-calls": 2,
        "no-sparse-arrays": 2,
        "no-unreachable": 2,
        "use-isnan": 2,
        "block-scoped-var": 2,
        "valid-typeof": 2,

        "array-callback-return": [2, {
            allowImplicit: true,
        }],

        "consistent-return": 1,
        curly: [2, "multi-line"],
        "default-case": 2,

        "dot-notation": [2, {
            allowKeywords: true,
        }],

        "linebreak-style": [2, "unix"],
        eqeqeq: 2,
        "guard-for-in": 0,
        "no-array-constructor": 2,
        "no-caller": 2,
        "no-else-return": 2,
        "no-eq-null": 2,
        "no-eval": 2,
        "no-extend-native": 2,
        "no-extra-bind": 2,
        "no-fallthrough": 2,
        "no-floating-decimal": 2,
        "no-implied-eval": 2,
        "no-lone-blocks": 2,
        "no-loop-func": 2,
        "no-multi-str": 2,
        "no-native-reassign": 2,
        "no-new": 2,
        "no-new-func": 2,
        "no-new-wrappers": 2,
        "no-octal": 2,
        "no-octal-escape": 2,
        "no-param-reassign": 2,
        "no-proto": 2,
        "no-prototype-builtins": 1,
        "no-redeclare": 2,
        "no-return-assign": 2,
        "no-script-url": 2,
        "no-self-compare": 2,
        "no-sequences": 2,
        "no-throw-literal": 2,
        "no-with": 2,
        radix: 2,
        "vars-on-top": 2,
        "wrap-iife": [2, "any"],

        "object-shorthand": [2, "always", {
            ignoreConstructors: true,
            avoidQuotes: true,
        }],

        "quote-props": [2, "as-needed", {
            keywords: true,
        }],

        yoda: 2,

        indent: [2, 4, {
            SwitchCase: 1,
        }],

        "brace-style": [2, "stroustrup", {
            allowSingleLine: true,
        }],

        quotes: [2, "double", "avoid-escape"],

        camelcase: [2, {
            properties: "never",
        }],

        "comma-spacing": [2, {
            before: false,
            after: true,
        }],

        "comma-style": [2, "last"],
        "eol-last": 2,
        "func-names": 0,

        "key-spacing": [2, {
            beforeColon: false,
            afterColon: true,
        }],

        "new-cap": [2, {
            newIsCap: true,
        }],

        "no-multiple-empty-lines": [2, {
            max: 2,
        }],

        "no-nested-ternary": 2,
        "no-new-object": 2,
        "no-spaced-func": 2,
        "no-trailing-spaces": 2,
        "no-extra-parens": [2, "functions"],
        "no-underscore-dangle": 0,
        "one-var": [2, "never"],
        "padded-blocks": [2, "never"],
        semi: [2, "always"],

        "semi-spacing": [2, {
            before: false,
            after: true,
        }],

        "space-after-keywords": 0,

        "space-before-blocks": [2, {
            functions: "never",
            keywords: "never",
            classes: "always",
        }],

        "keyword-spacing": [0, {
            before: false,
            after: true,
        }],

        "space-before-function-paren": [2, "never"],
        "space-infix-ops": 2,
        "space-return-throw-case": 0,
        "spaced-comment": 2,
    },
}];
