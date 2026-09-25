import { createFileRoute, Link, useNavigate, useRouter } from "@tanstack/react-router";
import { useEffect, useState } from "react";

import { StepIndicator } from "@/components/civic/StepIndicator";
import { CodeStep, RESEND_AFTER_SECONDS } from "@/components/join/CodeStep";
import { ConsentStep } from "@/components/join/ConsentStep";
import { IdentityStep } from "@/components/join/IdentityStep";
import { ProfileStep } from "@/components/join/ProfileStep";
import { type WardChoice, WardStep } from "@/components/join/WardStep";
import { button } from "@/components/ui/Button";
import { describeError, fieldErrors } from "@/lib/api-errors";
import { loadBoundaries } from "@/lib/boundaries";
import { useT } from "@/lib/i18n/I18nProvider";
import type { Lang } from "@/lib/i18n/lang";
import type { MessageKey } from "@/lib/i18n/messages";
import { CONSENT_VERSION, normalizeKenyanMobile } from "@/lib/validation";
import { callApiEither } from "@/runtimes/get-runtime";

export const Route = createFileRoute("/join")({ component: Join });

const STEPS = ["consent", "identity", "ward", "profile", "verify"] as const;
type Step = (typeof STEPS)[number];
const STEP_NAME: Record<Step, MessageKey> = {
	consent: "join.consent.name",
	identity: "join.identity.name",
	ward: "join.ward.name",
	profile: "join.profile.name",
	verify: "join.verify.name",
};

/**
 * W1.3.1 — registration: consent → ID + phone → ward → profile → SMS code.
 * The national ID lives only in this component's memory (never storage).
 * The session starts only after the SMS code proves the phone.
 */
function Join() {
	const { t, lang } = useT();
	const navigate = useNavigate();
	const router = useRouter();
	const [step, setStep] = useState<Step>("consent");
	// Fetch the ward list while the member reads the consent and types their ID,
	// so the ward step opens ready.
	useEffect(() => {
		loadBoundaries().catch(() => undefined);
	}, []);
	const [consent, setConsent] = useState(false);
	const [identity, setIdentity] = useState({ nationalId: "", phone: "" });
	const [identityErrors, setIdentityErrors] = useState<{ nationalId?: string; phone?: string }>({});
	const [ward, setWard] = useState<WardChoice | undefined>();
	const [profile, setProfile] = useState<{ displayName: string; preferredLang: Lang }>({
		displayName: "",
		preferredLang: lang,
	});
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const [alreadyRegistered, setAlreadyRegistered] = useState(false);

	const go = (next: Step) => {
		setError(undefined);
		setStep(next);
		window.scrollTo?.({ top: 0 });
	};
	const phone = normalizeKenyanMobile(identity.phone) ?? identity.phone;

	const requestCode = async (): Promise<number> => {
		const r = await callApiEither((api) =>
			api.auth.requestOtp({ payload: { nationalId: identity.nationalId } }),
		);
		if (r._tag === "Left") {
			setError(describeError(r.left, t));
			return r.left._tag === "RateLimited" ? r.left.retryAfter : RESEND_AFTER_SECONDS;
		}
		return RESEND_AFTER_SECONDS;
	};

	const register = async (values: typeof profile) => {
		if (!ward) return go("ward");
		setProfile(values);
		setBusy(true);
		setError(undefined);
		setAlreadyRegistered(false);
		const r = await callApiEither((api) =>
			api.auth.register({
				payload: {
					nationalId: identity.nationalId,
					displayName: values.displayName,
					preferredLang: values.preferredLang,
					phone,
					wardId: ward.code,
					consentVersion: CONSENT_VERSION,
					consentGranted: true,
				},
			}),
		).catch((e: unknown) => ({ _tag: "Left" as const, left: e }));
		setBusy(false);

		if (r._tag === "Right") {
			go("verify");
			await requestCode();
			return;
		}
		const e = r.left as { _tag?: string; code?: string; detail?: string };
		const fields = fieldErrors(e);
		if (e._tag === "Conflict") {
			setAlreadyRegistered(true);
			setError(t("join.error.alreadyRegistered"));
		} else if (e._tag === "InvalidInput" || fields.phone || fields.national_id) {
			setIdentityErrors({
				...(e._tag === "InvalidInput" || fields.national_id
					? { nationalId: e.detail || t("join.identity.idError") }
					: {}),
				...(fields.phone ? { phone: t("join.identity.phoneError") } : {}),
			});
			go("identity");
		} else if (e.code === "invalid_unit" || fields.ward_id) {
			go("ward");
			setError(describeError(e, t));
		} else if (e.code === "consent_required") {
			setConsent(false);
			go("consent");
		} else {
			setError(describeError(e, t));
		}
	};

	const verify = async (code: string) => {
		setBusy(true);
		setError(undefined);
		const r = await callApiEither((api) =>
			api.auth.login({ payload: { nationalId: identity.nationalId, otp: code } }),
		).catch((e: unknown) => ({ _tag: "Left" as const, left: e }));
		setBusy(false);
		if (r._tag === "Right") {
			await router.invalidate();
			await navigate({ to: "/" });
			return;
		}
		const e = r.left as { _tag?: string };
		setError(e._tag === "Unauthorized" ? t("join.verify.wrongCode") : describeError(e, t));
	};

	const index = STEPS.indexOf(step);
	return (
		<main id="main" className="mx-auto flex max-w-xl flex-col gap-6 px-4 py-8">
			<h1 className="sr-only">{t("join.title")}</h1>
			<StepIndicator current={index + 1} total={STEPS.length} name={t(STEP_NAME[step])} />

			{step === "consent" && (
				<ConsentStep
					agreed={consent}
					onNext={() => {
						setConsent(true);
						go("identity");
					}}
				/>
			)}
			{step === "identity" && (
				<IdentityStep
					initial={identity}
					serverErrors={identityErrors}
					onBack={() => go("consent")}
					onNext={(values) => {
						setIdentity(values);
						setIdentityErrors({});
						go("ward");
					}}
				/>
			)}
			{step === "ward" && (
				<>
					{error && (
						<p role="alert" className="rounded-md bg-danger/10 p-3 text-body text-danger">
							{error}
						</p>
					)}
					<WardStep
						initial={ward}
						onBack={() => go("identity")}
						onNext={(choice) => {
							setWard(choice);
							go("profile");
						}}
					/>
				</>
			)}
			{step === "profile" && (
				<>
					<ProfileStep
						initial={profile}
						busy={busy}
						error={error}
						onBack={() => go("ward")}
						onSubmit={register}
					/>
					{alreadyRegistered && (
						<Link to="/login" className={button({ variant: "secondary", full: true })}>
							{t("join.error.goLogin")}
						</Link>
					)}
				</>
			)}
			{step === "verify" && (
				<CodeStep phone={phone} busy={busy} error={error} onSubmit={verify} onResend={requestCode} />
			)}
		</main>
	);
}
