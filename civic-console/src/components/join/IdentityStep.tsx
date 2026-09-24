import { useId, useState } from "react";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { useT } from "@/lib/i18n/I18nProvider";
import { digitsOnly, isNationalId, normalizeKenyanMobile } from "@/lib/validation";

type Values = { nationalId: string; phone: string };

/**
 * T-W1.3.1.3 — national ID (masked, numeric keypad, never autofilled or
 * stored) and the mobile number the SMS code goes to.
 */
export function IdentityStep({
	initial,
	serverErrors,
	onBack,
	onNext,
}: {
	initial: Values;
	serverErrors?: Partial<Record<keyof Values, string>>;
	onBack: () => void;
	onNext: (values: Values) => void;
}) {
	const { t } = useT();
	const [nationalId, setNationalId] = useState(initial.nationalId);
	const [phone, setPhone] = useState(initial.phone);
	const [show, setShow] = useState(false);
	const [touched, setTouched] = useState(false);
	const idInput = useId();

	const idError =
		touched && !isNationalId(nationalId) ? t("join.identity.idError") : serverErrors?.nationalId;
	const phoneError =
		touched && !normalizeKenyanMobile(phone) ? t("join.identity.phoneError") : serverErrors?.phone;

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(e) => {
				e.preventDefault();
				setTouched(true);
				if (isNationalId(nationalId) && normalizeKenyanMobile(phone)) onNext({ nationalId, phone });
			}}
		>
			<div className="flex flex-col gap-1">
				<Input
					id={idInput}
					label={t("join.identity.id")}
					hint={t("join.identity.idHint")}
					error={idError}
					type={show ? "text" : "password"}
					inputMode="numeric"
					autoComplete="off"
					spellCheck={false}
					maxLength={8}
					value={nationalId}
					onChange={(e) => setNationalId(digitsOnly(e.target.value, 8))}
				/>
				<Button
					variant="ghost"
					size="sm"
					aria-controls={idInput}
					aria-pressed={show}
					onClick={() => setShow((s) => !s)}
					className="w-fit"
				>
					{show ? t("common.hide") : t("common.show")}
				</Button>
			</div>
			<Input
				label={t("join.identity.phone")}
				hint={t("join.identity.phoneHint")}
				error={phoneError}
				type="tel"
				inputMode="tel"
				autoComplete="tel"
				value={phone}
				onChange={(e) => setPhone(e.target.value)}
			/>
			<div className="flex gap-3">
				<Button variant="secondary" onClick={onBack}>
					{t("common.back")}
				</Button>
				<Button type="submit" className="flex-1">
					{t("common.continue")}
				</Button>
			</div>
		</form>
	);
}
