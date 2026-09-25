import { useEffect, useState } from "react";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { useT } from "@/lib/i18n/I18nProvider";
import { digitsOnly, isOtp } from "@/lib/validation";

export const RESEND_AFTER_SECONDS = 60;

/** "+254712345678" → "+254 7•• ••• 678": enough to recognise, not to copy. */
export function maskPhone(e164: string): string {
	const d = e164.replace(/\D/g, "");
	if (d.length !== 12) return "•••";
	return `+${d.slice(0, 3)} ${d[3]}•• ••• ${d.slice(9)}`;
}

/**
 * T-W1.3.1.4 / T-W1.3.2.1 — enter the SMS code, with a resend countdown.
 * Used by registration (phone verification) and login.
 */
export function CodeStep({
	phone,
	busy,
	error,
	onSubmit,
	onResend,
	onBack,
}: {
	/** E.164 number the code went to, if known (registration). */
	phone?: string | undefined;
	busy: boolean;
	error?: string | undefined;
	onSubmit: (code: string) => void;
	/** Resolves to seconds to wait before the next resend. */
	onResend: () => Promise<number>;
	onBack?: () => void;
}) {
	const { t } = useT();
	const [code, setCode] = useState("");
	const [touched, setTouched] = useState(false);
	const [wait, setWait] = useState(RESEND_AFTER_SECONDS);
	const [resent, setResent] = useState(false);

	useEffect(() => {
		if (wait <= 0) return;
		const timer = setTimeout(() => setWait((w) => w - 1), 1000);
		return () => clearTimeout(timer);
	}, [wait]);

	const codeError = touched && !isOtp(code) ? t("join.verify.codeError") : undefined;

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(e) => {
				e.preventDefault();
				setTouched(true);
				if (isOtp(code)) onSubmit(code);
			}}
		>
			<h2 className="text-h1">{t("join.verify.heading")}</h2>
			{phone && <p className="text-body">{t("join.verify.sentTo", { phone: maskPhone(phone) })}</p>}
			<Input
				label={t("join.verify.code")}
				error={codeError}
				inputMode="numeric"
				autoComplete="one-time-code"
				maxLength={6}
				className="max-w-48 font-mono tracking-widest"
				value={code}
				onChange={(e) => setCode(digitsOnly(e.target.value, 6))}
			/>
			{error && (
				<p role="alert" className="rounded-md bg-danger/10 p-3 text-body text-danger">
					{error}
				</p>
			)}
			<Button type="submit" full disabled={busy} aria-busy={busy}>
				{t("join.verify.submit")}
			</Button>
			<div className="flex flex-col gap-2">
				<Button
					variant="ghost"
					disabled={wait > 0 || busy}
					onClick={async () => {
						setResent(false);
						const next = await onResend();
						setWait(next);
						setResent(true);
					}}
				>
					{t("join.verify.resend")}
				</Button>
				<p className="text-small text-muted" aria-live="polite">
					{[resent && t("join.verify.resent"), wait > 0 && t("join.verify.resendIn", { seconds: wait })]
						.filter(Boolean)
						.join(" ")}
				</p>
			</div>
			{onBack && (
				<Button variant="secondary" onClick={onBack} disabled={busy}>
					{t("common.back")}
				</Button>
			)}
		</form>
	);
}
