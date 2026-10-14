import { mount } from "svelte";
import "./app.css";
import { loadBranding } from "./lib/branding.svelte.js";
import App from "./App.svelte";

loadBranding();
mount(App, { target: document.getElementById("app") });
