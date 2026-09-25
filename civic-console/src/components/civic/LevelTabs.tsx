import { type KeyboardEvent, useRef } from "react";

import type { Level } from "@/api/api-contract";
import { LEVELS, LevelIndicator } from "@/components/ui/LevelIndicator";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";

type Props = {
	value: Level;
	onChange: (level: Level) => void;
	/** id of the tabpanel the tabs control. */
	panelId: string;
};

export const tabId = (level: Level) => `level-tab-${level}`;

/**
 * T-W1.4.1.1 — Ward / Constituency / County / National tabs (WAI-ARIA tabs:
 * one tab in the tab order, arrows/Home/End move and select). Four equal
 * segments that always fit: on phones the dots sit above a smaller label.
 */
export function LevelTabs({ value, onChange, panelId }: Props) {
	const { t } = useT();
	const refs = useRef(new Map<Level, HTMLButtonElement>());

	const move = (e: KeyboardEvent, index: number) => {
		const last = LEVELS.length - 1;
		const target = { ArrowRight: index + 1, ArrowLeft: index - 1, Home: 0, End: last }[e.key];
		if (target === undefined) return;
		e.preventDefault();
		const level = LEVELS[(target + LEVELS.length) % LEVELS.length] as Level;
		refs.current.get(level)?.focus();
		onChange(level);
	};

	return (
		<div
			role="tablist"
			aria-label={t("feed.tabs")}
			className="grid grid-cols-4 gap-1 rounded-full border border-border bg-surface-2 p-1"
		>
			{LEVELS.map((level, i) => {
				const selected = level === value;
				return (
					<button
						key={level}
						ref={(el) => {
							if (el) refs.current.set(level, el);
							else refs.current.delete(level);
						}}
						type="button"
						role="tab"
						id={tabId(level)}
						aria-selected={selected}
						aria-controls={panelId}
						tabIndex={selected ? 0 : -1}
						onClick={() => onChange(level)}
						onKeyDown={(e) => move(e, i)}
						className={cn(
							"flex min-h-11 min-w-0 flex-col items-center justify-center gap-1 rounded-full px-1 text-micro font-medium",
							"sm:flex-row sm:gap-2 sm:px-3 sm:text-small",
							"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
							selected ? "bg-kenya-green text-on-accent" : "text-ink hover:bg-paper",
						)}
					>
						<span aria-hidden="true" className="inline-flex">
							<LevelIndicator current={level} inverse={selected} />
						</span>
						<span className="max-w-full truncate">{t(`level.${level}`)}</span>
					</button>
				);
			})}
		</div>
	);
}
