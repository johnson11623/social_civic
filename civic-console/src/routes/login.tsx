import { createFileRoute, Link, useNavigate, useRouter } from "@tanstack/react-router";
import { useState } from "react";

import { NationalIdInput } from "@/components/civic/NationalIdInput";
import { CodeStep, RESEND_AFTER_SECONDS } from "@/components/join/CodeStep";
import { Button } from "@/components/ui/Button";
import { describeError } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { isNationalId } from "@/lib/validation";
import { callApiEither } from "@/runtimes/get-runtime";

export const Route = createFileRoute("/login")({ component: Login });

/** T-W1.3.2.1 — log in with national ID + SMS code. */
function Login() {
	const { t } = useT();
	const navigate = useNavigate();
	const router = useRouter();
	const [nationalId, setNationalId] = useState("");
	const [touched, setTouched] = useState(false);
	const [step, setStep] = useState<"id" | "code">("id");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | undefined>();

	const requestCode = async (): Promise<number> => {
		setBusy(true);
		setError(undefined);
		const r = await callApiEither((api) => api.auth.requestOtp({ payload: { nationalId } })).catch(
			(e: unknown) => ({ _tag: "Left" as const, left: e }),
		);
		setBusy(false);
		if (r._tag === "Right") {
			setStep("code");
			return RESEND_AFTER_SECONDS;
		}
		setError(describeError(r.left, t));
		const e = r.left as { _tag?: string; retryAfter?: number };
		return e._tag === "RateLimited" && e.retryAfter ? e.retryAfter : RESEND_AFTER_SECONDS;
	};

	const login = async (otp: string) => {
		setBusy(true);
		setError(undefined);
		const r = await callApiEither((api) => api.auth.login({ payload: { nationalId, otp } })).catch(
			(e: unknown) => ({ _tag: "Left" as const, left: e }),
		);
		setBusy(false);
		if (r._tag === "Right") {
			await router.invalidate();
			await navigate({ to: "/" });
			return;
		}
		const e = r.left as { _tag?: string };
		setError(e._tag === "Unauthorized" ? t("join.verify.wrongCode") : describeError(e, t));
	};

	return (
		<main id="main" className="mx-auto flex max-w-xl flex-col gap-6 px-4 py-8">
			<h1 className="text-h1">{t("login.title")}</h1>
			{step === "id" ? (
				<form
					noValidate
					className="flex flex-col gap-6"
					onSubmit={(e) => {
						e.preventDefault();
						setTouched(true);
						if (isNationalId(nationalId)) void requestCode();
					}}
				>
					<p className="text-body text-muted">{t("login.lead")}</p>
					<NationalIdInput
						value={nationalId}
						onChange={setNationalId}
						error={touched && !isNationalId(nationalId) ? t("join.identity.idError") : undefined}
					/>
					{error && (
						<p role="alert" className="rounded-md bg-danger/10 p-3 text-body text-danger">
							{error}
						</p>
					)}
					<Button type="submit" full disabled={busy} aria-busy={busy}>
						{t("login.sendCode")}
					</Button>
					<p className="text-small text-muted">
						{t("login.noAccount")}{" "}
						<Link to="/join" className="text-accent underline underline-offset-2">
							{t("landing.join")}
						</Link>
					</p>
				</form>
			) : (
				<CodeStep
					busy={busy}
					error={error}
					onSubmit={login}
					onResend={requestCode}
					onBack={() => {
						setError(undefined);
						setStep("id");
					}}
				/>
			)}
		</main>
	);
}
