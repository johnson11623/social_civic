import { TanStackDevtools } from "@tanstack/react-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";

/** Development-only devtools, loaded lazily from __root.tsx. */
export default function Devtools() {
	return (
		<TanStackDevtools
			config={{ position: "bottom-right" }}
			plugins={[{ name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> }]}
		/>
	);
}
