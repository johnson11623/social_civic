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
 * one tab in the tab order, arrows/Home/End move and select). Horizontal
 * pills that scroll on narrow screens.
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
		<div role="tablist" aria-label={t("feed.tabs")} className="-mx-4 flex gap-2 overflow-x-auto px-4 pb-1">
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
							"inline-flex min-h-11 shrink-0 items-center gap-2 rounded-full border px-4 text-small font-medium",
							"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
							selected
								? "border-kenya-green bg-kenya-green text-on-accent"
								: "border-border bg-paper text-ink hover:bg-surface-2",
						)}
					>
						<span aria-hidden="true" className="inline-flex">
							<LevelIndicator current={level} inverse={selected} />
						</span>
						{t(`level.${level}`)}
					</button>
				);
			})}
		</div>
	);
}
