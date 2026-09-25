import { useState } from "react";

import { KSLVideo } from "@/components/accessibility/KSLVideo";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Modal } from "@/components/ui/Modal";
import { useT } from "@/lib/i18n/I18nProvider";
import type { MessageKey } from "@/lib/i18n/messages";

const NOTICE: MessageKey[] = [
	"join.consent.purpose",
	"join.consent.storage",
	"join.consent.access",
	"join.consent.rights",
];

/**
 * T-W1.3.1.2 — plain-language notice (purpose, storage, access, rights; US-15)
 * and explicit opt-in: the box starts unticked and must be ticked to go on.
 */
export function ConsentStep({ agreed, onNext }: { agreed: boolean; onNext: () => void }) {
	const { t } = useT();
	const [checked, setChecked] = useState(agreed);
	const [error, setError] = useState(false);
	const [why, setWhy] = useState(false);

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(e) => {
				e.preventDefault();
				if (!checked) return setError(true);
				onNext();
			}}
		>
			<h2 className="text-h1">{t("join.consent.heading")}</h2>
			<p className="text-body">{t("join.consent.intro")}</p>
			<ul className="flex list-disc flex-col gap-2 pl-6 text-body">
				{NOTICE.map((key) => (
					<li key={key}>{t(key)}</li>
				))}
			</ul>
			<button
				type="button"
				onClick={() => setWhy(true)}
				className="w-fit text-body text-accent underline underline-offset-2 hover:text-accent-hover"
			>
				{t("join.consent.why")}
			</button>
			<Checkbox
				label={t("join.consent.checkbox")}
				checked={checked}
				onChange={(e) => {
					setChecked(e.target.checked);
					if (e.target.checked) setError(false);
				}}
				error={error ? t("join.consent.required") : undefined}
			/>
			<Button type="submit" full>
				{t("common.continue")}
			</Button>

			<Modal open={why} onClose={() => setWhy(false)} title={t("join.consent.why")}>
				<p>{t("join.consent.intro")}</p>
				<ul className="flex list-disc flex-col gap-2 pl-6">
					{NOTICE.map((key) => (
						<li key={key}>{t(key)}</li>
					))}
				</ul>
				<KSLVideo />
			</Modal>
		</form>
	);
}
