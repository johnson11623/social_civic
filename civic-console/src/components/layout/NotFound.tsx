import { Link } from "@tanstack/react-router";

/**
 * Router-wide 404. Shows both languages until the i18n bundles land
 * (T-W1.1.3.2 moves these strings into useT()).
 */
export function NotFound() {
	return (
		<main id="main" className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-16">
			<p className="text-micro uppercase tracking-wide text-muted">404</p>
			<h1 className="text-h1 text-ink">
				<span lang="sw">Ukurasa haukupatikana</span>
				<span className="text-muted"> · </span>
				<span lang="en">Page not found</span>
			</h1>
			<p className="text-body text-muted">
				<span lang="sw">Kiungo hiki hakipo au kimehamishwa.</span>{" "}
				<span lang="en">This link doesn't exist or has moved.</span>
			</p>
			<Link
				to="/"
				className="inline-flex min-h-11 w-fit items-center rounded-sm bg-accent px-4 text-body font-medium text-on-accent hover:bg-accent-hover"
			>
				<span lang="sw">Rudi mwanzo</span>
				<span aria-hidden="true">&nbsp;·&nbsp;</span>
				<span lang="en">Go home</span>
			</Link>
		</main>
	);
}
