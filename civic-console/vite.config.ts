import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// T-W1.1.1.2 — TanStack Start + React + Tailwind, `@/` alias from tsconfig
// paths, and long-lived vendor chunks so app deploys don't bust the cache
// for Effect and React (3G budget: ≤ 150KB initial JS).
export default defineConfig({
	resolve: { tsconfigPaths: true },
	plugins: [
		devtools(),
		tailwindcss(),
		tanstackStart({ srcDirectory: "src", router: { routesDirectory: "routes" } }),
		viteReact(),
	],
	build: {
		rolldownOptions: {
			output: {
				// Rolldown's replacement for Rollup's manualChunks object form.
				codeSplitting: {
					groups: [
						{ name: "effect", test: /[\\/]node_modules[\\/](\.pnpm[\\/])?(effect|@effect)[@\\/]/ },
						{
							name: "vendor",
							test: /[\\/]node_modules[\\/](\.pnpm[\\/])?(react|react-dom|scheduler)[@\\/]/,
						},
					],
				},
			},
		},
	},
});
