import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

const target = process.env.API_TARGET || "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [
    svelte(),
    tailwindcss(),
    {
      // the build empties dist/; restore the tracked placeholder
      name: "keep-gitkeep",
      closeBundle() {
        writeFileSync(resolve(import.meta.dirname, "dist/.gitkeep"), "");
      },
    },
  ],
  server: {
    proxy: { "/api": target },
  },
});
