import js from "@eslint/js";
import {
    defineConfig,
    globalIgnores,
} from "eslint/config";
import ts from "typescript-eslint";

export default defineConfig(
    globalIgnores([".wrangler/"]),
    js.configs.recommended,
    ts.configs.recommended,
    {
        files: ["src/**/*.ts"],
        languageOptions: {
            parserOptions: {
                projectService: true,
                tsconfigRootDir: import.meta.dirname,
            },
        },
        // The promise rules from the type-checked set, as in apps/web. A promise nobody awaits escapes
        // the handler's try/catch, so its failure rejects nothing and the mail is lost without a bounce.
        rules: {
            "@typescript-eslint/await-thenable": "error",
            "@typescript-eslint/no-floating-promises": "error",
            "@typescript-eslint/no-misused-promises": "error",
        },
    },
);
