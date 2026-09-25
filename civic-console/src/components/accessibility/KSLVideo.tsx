import { useT } from "@/lib/i18n/I18nProvider";

type Props = {
	/** From the Accessibility service; absent until produced. Captions are mandatory (US-12). */
	video?: { src: string; captions: { en: string; sw: string } } | undefined;
};

/**
 * Kenyan Sign Language summary (US-12). Plays only on request (no autoplay)
 * with EN/SW captions. Until a video exists for a notice, says so plainly.
 */
export function KSLVideo({ video }: Props) {
	const { t, lang } = useT();
	return (
		<section aria-label={t("join.consent.ksl")} className="flex flex-col gap-2 rounded-md bg-surface p-4">
			<p className="text-small font-medium text-ink">{t("join.consent.ksl")}</p>
			{video ? (
				<video controls preload="none" className="w-full rounded-sm" src={video.src}>
					<track
						kind="captions"
						srcLang="sw"
						src={video.captions.sw}
						label="Kiswahili"
						default={lang === "sw"}
					/>
					<track
						kind="captions"
						srcLang="en"
						src={video.captions.en}
						label="English"
						default={lang === "en"}
					/>
				</video>
			) : (
				<p className="text-small text-muted">{t("join.consent.kslUnavailable")}</p>
			)}
		</section>
	);
}
