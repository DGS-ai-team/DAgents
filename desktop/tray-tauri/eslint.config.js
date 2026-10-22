import js from "@eslint/js";
import globals from "globals";
import vue from "eslint-plugin-vue";

export default [
  { ignores: ["dist/**", "node_modules/**"] },
  js.configs.recommended,
  ...vue.configs["flat/recommended"],
  {
    files: ["**/*.{js,vue}"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      "no-console": "off",
      "no-unused-vars": ["error", { args: "none", ignoreRestSiblings: true }],
      "vue/multi-word-component-names": "off",
      "vue/no-v-html": "off",
      "vue/require-default-prop": "off",
    },
  },
];
