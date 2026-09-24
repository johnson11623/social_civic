import { useState } from "react";

import { useT } from "@/lib/i18n/I18nProvider";
import { LANGS } from "@/lib/i18n/lang";

/**
 * EN/SW toggle, available on every screen (Web App Design §8). Each option is
 * labelled in its own language so either reader can find theirs.
 */
export function LanguageToggle() {
	const { lang, setLang, t } = useT();
	const [changed, setChanged] = useState(false);
	return (
		<fieldset className="inline-flex items-center gap-1 rounded-sm border border-border p-1">
			<legend className="sr-only">{t("language.label")}</legend>
			{LANGS.map((code) => (
				<button
					key={code}
					type="button"
					lang={code}
					aria-pressed={lang === code}
					onClick={() => {
						if (lang === code) return;
						setLang(code);
						setChanged(true);
					}}
					className="min-h-11 min-w-11 rounded-sm px-3 text-small font-medium text-muted hover:bg-surface-2 aria-pressed:bg-accent aria-pressed:text-on-accent"
				>
					{t(`language.${code}`)}
				</button>
			))}
			{/* Announced to screen readers in the new language after a switch. */}
			<span role="status" className="sr-only">
				{changed ? t("language.changed") : ""}
			</span>
		</fieldset>
	);
}
