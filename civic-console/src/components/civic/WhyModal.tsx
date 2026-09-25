import type { Post } from "@/api/api-contract";
import { Modal } from "@/components/ui/Modal";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * T-W1.4.1.5 — "Why am I seeing this?": origin, current level, score and the
 * activity behind it, with a plain explanation. Transparency is a
 * first-class feature (Web App Design, Flow 3).
 */
export function WhyModal({ post, onClose }: { post: Post | null; onClose: () => void }) {
	const { t, lang } = useT();
	const elevated = post !== null && post.level !== "ward";
	return (
		<Modal open={post !== null} onClose={onClose} title={t("why.title")}>
			{post && (
				<>
					<dl className="flex flex-col gap-2 text-small">
						<div className="flex justify-between gap-4">
							<dt className="text-muted">{t("why.origin")}</dt>
							<dd>{t("level.ward")}</dd>
						</div>
						<div className="flex justify-between gap-4">
							<dt className="text-muted">{t("why.current")}</dt>
							<dd>{t(`level.${post.level}`)}</dd>
						</div>
						<div className="flex justify-between gap-4">
							<dt className="text-muted">{t("why.score")}</dt>
							<dd>{formatNumber(Math.round(post.score * 10) / 10, lang)}</dd>
						</div>
						<div className="flex justify-between gap-4">
							<dt className="text-muted">{t("why.activity")}</dt>
							<dd className="text-right">
								{t("why.likes", { count: formatNumber(post.counts.likes, lang) })} ·{" "}
								{t("why.replies", { count: formatNumber(post.counts.replies, lang) })}
							</dd>
						</div>
					</dl>
					<p>
						{elevated
							? t("why.elevated", { from: t("level.ward"), to: t(`level.${post.level}`) })
							: t("why.ward")}
					</p>
				</>
			)}
		</Modal>
	);
}
