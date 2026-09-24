import { LanguageToggle } from "@/components/civic/LanguageToggle";

/**
 * The EN/SW switch for visitors who haven't signed in (landing, join, log
 * in), so they can still change language (Web App Design §8). Signed-in
 * users have it on their account page instead.
 */
export function SiteFooter() {
	return (
		<footer className="border-t border-border bg-paper">
			<div className="mx-auto flex max-w-7xl items-center justify-end px-4 py-3">
				<LanguageToggle />
			</div>
		</footer>
	);
}
