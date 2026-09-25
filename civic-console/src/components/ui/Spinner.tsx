import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";

/** Atom: Spinner (T-W1.2.2.5) — announced as a status; label defaults to "Loading". */
export function Spinner({ label, className }: { label?: string; className?: string }) {
	const { t } = useT();
	return (
		<span role="status" aria-live="polite" className={cn("inline-flex", className)}>
			<svg className="h-5 w-5 animate-spin text-accent" viewBox="0 0 24 24" aria-hidden="true">
				<circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" fill="none" opacity="0.25" />
				<path d="M22 12a10 10 0 0 1-10 10" stroke="currentColor" strokeWidth="3" fill="none" />
			</svg>
			<span className="sr-only">{label ?? t("common.loading")}</span>
		</span>
	);
}
