import type { Post } from "@/api/api-contract";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * T-W1.4.1.4 — the disclosure on a sponsored post: its fixed English and
 * Kiswahili labels, always both, on a distinct tint (role=note).
 */
export function SponsoredLabel({ label }: { label: NonNullable<Post["sponsoredLabel"]> }) {
	const { t } = useT();
	return (
		<div
			role="note"
			aria-label={t("sponsored.heading")}
			className="rounded-sm bg-sponsored px-3 py-2 text-small text-ink"
		>
			<span lang="en">{label.en}</span>
			<span aria-hidden="true"> · </span>
			<span lang="sw">{label.sw}</span>
		</div>
	);
}
