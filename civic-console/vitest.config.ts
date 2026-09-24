import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// T-W1.1.1.8 — unit and component tests. Kept separate from vite.config.ts so
// the TanStack Start plugin (SSR, route generation) doesn't run under test.
export default defineConfig({
	resolve: { tsconfigPaths: true },
	plugins: [viteReact()],
	test: {
		environment: "jsdom",
		include: [
			"src/**/*.test.{ts,tsx}",
			"tests/unit/**/*.test.{ts,tsx}",
			"tests/integration/**/*.test.{ts,tsx}",
		],
		setupFiles: ["./tests/setup.ts"],
		css: false,
		coverage: {
			provider: "v8",
			include: ["src/**/*.{ts,tsx}"],
			exclude: ["src/routeTree.gen.ts", "src/**/*.test.{ts,tsx}"],
		},
	},
});
