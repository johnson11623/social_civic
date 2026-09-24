import { LanguageToggle } from "@/components/civic/LanguageToggle";

/**
 * Bottom of every screen: the EN/SW switch, so visitors who haven't signed
 * in can still change language (Web App Design §8). Signed-in users also
 * have it on their account page.
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
