import type { Level } from "@/api/api-contract";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { LEVELS } from "@/lib/levels";

export { LEVELS } from "@/lib/levels";

/**
 * Atom: LevelIndicator (T-W1.2.2.6) — the 4-dot motif: Ward ●○○○ →
 * National ●●●●. The dots are decorative; the level is announced as text.
 */
export function LevelIndicator({
	current,
	className,
	inverse = false,
}: {
	current: Level;
	className?: string;
	/** On a filled (green/accent) background. */
	inverse?: boolean;
}) {
	const { t } = useT();
	const index = LEVELS.indexOf(current);
	return (
		<span
			role="img"
			aria-label={t("level.label", { level: t(`level.${current}`) })}
			className={cn("inline-flex items-center gap-1", className)}
		>
			{LEVELS.map((level, i) => (
				<span
					key={level}
					aria-hidden="true"
					className={cn(
						"h-2 w-2 rounded-full",
						inverse
							? i <= index
								? "bg-on-accent"
								: "bg-on-accent/40"
							: i <= index
								? "bg-kenya-green"
								: "bg-border",
					)}
				/>
			))}
		</span>
	);
}
