import { useT } from "@/lib/i18n/I18nProvider";

/** "Step 2 of 5: Your ID and phone" with a progress bar (wizard orientation). */
export function StepIndicator({ current, total, name }: { current: number; total: number; name: string }) {
	const { t } = useT();
	return (
		<div className="flex flex-col gap-2">
			<p className="text-small text-muted">{t("join.step", { current, total, name })}</p>
			<div className="flex gap-1" aria-hidden="true">
				{Array.from({ length: total }, (_, i) => (
					<span
						// biome-ignore lint/suspicious/noArrayIndexKey: fixed-length decorative segments
						key={i}
						className={
							i < current ? "h-1 flex-1 rounded-full bg-kenya-green" : "h-1 flex-1 rounded-full bg-border"
						}
					/>
				))}
			</div>
		</div>
	);
}
