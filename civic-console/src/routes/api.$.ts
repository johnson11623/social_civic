import { createFileRoute } from "@tanstack/react-router";

import { apiHandler } from "@/runtimes/server-runtime.server";

// T-W1.1.2.6 — mounts the Effect API at /api/*. The config must be written
// inline: the route generator analyses it statically.
export const Route = createFileRoute("/api/$")({
	server: {
		handlers: {
			GET: apiHandler,
			POST: apiHandler,
			PUT: apiHandler,
			PATCH: apiHandler,
			DELETE: apiHandler,
			OPTIONS: apiHandler,
		},
	},
});
