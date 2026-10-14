import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

const target = process.env.API_TARGET || "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  server: {
    proxy: { "/api": target },
  },
});
