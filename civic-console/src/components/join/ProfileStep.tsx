import { useState } from "react";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { useT } from "@/lib/i18n/I18nProvider";
import type { Lang } from "@/lib/i18n/lang";
import { displayNameError } from "@/lib/validation";

type Values = { displayName: string; preferredLang: Lang };

/** T-W1.3.1.6 — display name and the language for SMS and notifications. */
export function ProfileStep({
	initial,
	busy,
	error,
	onBack,
	onSubmit,
}: {
	initial: Values;
	busy: boolean;
	error?: string | undefined;
	onBack: () => void;
	onSubmit: (values: Values) => void;
}) {
	const { t } = useT();
	const [displayName, setDisplayName] = useState(initial.displayName);
	const [preferredLang, setPreferredLang] = useState<Lang>(initial.preferredLang);
	const [touched, setTouched] = useState(false);

	const nameCode = touched ? displayNameError(displayName) : undefined;
	const nameError =
		nameCode === "required"
			? t("join.profile.displayNameRequired")
			: nameCode === "too_long"
				? t("join.profile.displayNameTooLong")
				: undefined;

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(e) => {
				e.preventDefault();
				setTouched(true);
				if (!displayNameError(displayName)) onSubmit({ displayName: displayName.trim(), preferredLang });
			}}
		>
			<Input
				label={t("join.profile.displayName")}
				hint={t("join.profile.displayNameHint")}
				error={nameError}
				autoComplete="nickname"
				maxLength={120}
				value={displayName}
				onChange={(e) => setDisplayName(e.target.value)}
			/>
			<fieldset className="flex flex-col gap-2">
				<legend className="mb-2 text-small font-medium text-ink">{t("join.profile.language")}</legend>
				{(["sw", "en"] as const).map((code) => (
					<label key={code} className="flex min-h-11 items-center gap-3 text-body">
						<input
							type="radio"
							name="preferredLang"
							value={code}
							checked={preferredLang === code}
							onChange={() => setPreferredLang(code)}
							className="h-5 w-5 accent-accent"
						/>
						<span lang={code}>{t(`language.${code}`)}</span>
					</label>
				))}
			</fieldset>
			{error && (
				<p role="alert" className="rounded-md bg-danger/10 p-3 text-body text-danger">
					{error}
				</p>
			)}
			<div className="flex gap-3">
				<Button variant="secondary" onClick={onBack} disabled={busy}>
					{t("common.back")}
				</Button>
				<Button type="submit" className="flex-1" disabled={busy} aria-busy={busy}>
					{t("join.profile.submit")}
				</Button>
			</div>
		</form>
	);
}
