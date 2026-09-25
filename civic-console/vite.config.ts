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
	// Dev server only: allow ngrok tunnels (testing on a phone); a leading dot
	// matches any subdomain. Extra hosts: VITE_ALLOWED_HOSTS=a.example,b.example
	server: {
		allowedHosts: [
			".ngrok-free.dev",
			".ngrok-free.app",
			".ngrok.app",
			...(process.env.VITE_ALLOWED_HOSTS?.split(",").filter(Boolean) ?? []),
		],
		// Media through this address (the API returns /media/variants/… and
		// /s3/… URLs in development), so other devices load and upload too.
		proxy: {
			"/media/variants": { target: `http://localhost:${process.env.MEDIA_CDN_PORT ?? "18080"}` },
			// changeOrigin sends storage's own Host, which the upload signature covers.
			"/s3": {
				target: `http://localhost:${process.env.S3_PORT ?? "19000"}`,
				changeOrigin: true,
				rewrite: (path) => path.replace(/^\/s3/, ""),
				// Tunnels (ngrok) add X-Forwarded-Host, which storage would sign-check
				// instead of its own host: drop them so the signature holds.
				configure: (proxy) =>
					proxy.on("proxyReq", (req) => {
						for (const h of ["x-forwarded-host", "x-forwarded-proto", "x-forwarded-port", "x-forwarded-for", "forwarded"])
							req.removeHeader(h);
					}),
			},
		},
	},
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
