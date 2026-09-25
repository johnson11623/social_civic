import { Link, useNavigate, useRouteContext, useRouter } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useEffect, useId, useRef, useState } from "react";

import type { Profile } from "@/api/api-contract";
import { LanguageToggle } from "@/components/civic/LanguageToggle";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { Select } from "@/components/ui/Select";
import { Switch } from "@/components/ui/Switch";
import { useToast } from "@/components/ui/Toast";
import { describeError, fieldErrors, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { DEFAULT_DISPLAY, type DisplayPrefs, saveDisplay, type Theme } from "@/lib/display";
import { formatDate } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import type { MessageKey } from "@/lib/i18n/messages";
import { MEDIA_TYPES } from "@/lib/limits";
import { checkFile, mediaKind, type UploadProgress, uploadFailureText, uploadMedia } from "@/lib/upload";
import { useLogout } from "@/lib/use-logout";
import { callApiEither } from "@/runtimes/get-runtime";

const SECTIONS: ReadonlyArray<{ id: string; title: MessageKey }> = [
	{ id: "profile", title: "account.profile" },
	{ id: "language", title: "account.language" },
	{ id: "display", title: "account.display" },
	{ id: "security", title: "account.security" },
	{ id: "privacy", title: "account.privacy" },
];

/**
 * The signed-in user's account: profile, language, display and
 * accessibility, two-step verification and privacy (consent, erasure).
 * Sections are listed in a nav so keyboard and screen-reader users can jump.
 */
export function AccountPage({ profile: initial }: { profile: Profile }) {
	const { t } = useT();
	const [profile, setProfile] = useState(initial);
	return (
		<main id="main" className="mx-auto flex max-w-5xl flex-col gap-6 px-4 py-6 pb-16">
			<Link to="/" className="w-fit text-small text-accent hover:text-accent-hover">
				<span aria-hidden="true">← </span>
				{t("account.back")}
			</Link>
			<header className="flex flex-col gap-1">
				<h1 className="text-h1">{t("account.title")}</h1>
				<p className="text-body text-muted">{t("account.body")}</p>
			</header>
			<div className="flex flex-col gap-6 lg:flex-row lg:items-start">
				<nav aria-label={t("account.nav")} className="lg:sticky lg:top-4 lg:w-56 lg:shrink-0">
					<ul className="-mx-4 flex gap-2 overflow-x-auto px-4 lg:mx-0 lg:flex-col lg:gap-1 lg:px-0">
						{SECTIONS.map((s) => (
							<li key={s.id} className="shrink-0">
								<a
									href={`#${s.id}`}
									className="flex min-h-11 items-center rounded-full border border-border px-4 text-small text-ink hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent lg:rounded-sm lg:border-0"
								>
									{t(s.title)}
								</a>
							</li>
						))}
					</ul>
				</nav>
				<div className="flex min-w-0 flex-1 flex-col gap-6">
					<ProfileSection profile={profile} onSaved={setProfile} />
					<LanguageSection profile={profile} onSaved={setProfile} />
					<DisplaySection />
					<SecuritySection
						profile={profile}
						onEnabled={() => setProfile((p) => ({ ...p, mfaEnabled: true }))}
					/>
					<PrivacySection profile={profile} onChange={setProfile} />
				</div>
			</div>
		</main>
	);
}

function Section({ id, titleKey, children }: { id: string; titleKey: MessageKey; children: ReactNode }) {
	const { t } = useT();
	return (
		<section
			id={id}
			aria-labelledby={`${id}-title`}
			className="flex scroll-mt-4 flex-col gap-4 rounded-md border border-border p-4 md:p-6"
		>
			<h2 id={`${id}-title`} className="text-h2">
				{t(titleKey)}
			</h2>
			{children}
		</section>
	);
}

// ---- Profile ---------------------------------------------------------------------------

type Update = { displayName?: string; preferredLang?: "en" | "sw" };

const saveProfile = (payload: Update) =>
	callApiEither((api) => api.account.updateProfile({ payload })).then(settle);

function ProfileSection({ profile, onSaved }: { profile: Profile; onSaved: (p: Profile) => void }) {
	const { t, lang } = useT();
	const toast = useToast();
	const router = useRouter();
	const [name, setName] = useState(profile.displayName);
	const [error, setError] = useState<string>();
	const [busy, setBusy] = useState(false);
	const changed = name.trim() !== profile.displayName;

	async function submit(e: FormEvent) {
		e.preventDefault();
		const trimmed = name.trim();
		if (trimmed.length === 0) return setError(t("join.profile.displayNameRequired"));
		setBusy(true);
		const res = await saveProfile({ displayName: trimmed });
		setBusy(false);
		if (!res.ok) {
			return setError(
				fieldErrors(res.error).display_name
					? t("join.profile.displayNameRequired")
					: describeError(res.error, t),
			);
		}
		setError(undefined);
		setName(res.value.displayName);
		onSaved(res.value);
		toast(t("account.saved"));
		void router.invalidate(); // the header avatar shows the new initials
	}

	return (
		<Section id="profile" titleKey="account.profile">
			<ProfilePhoto profile={profile} onSaved={onSaved} />
			<form onSubmit={submit} noValidate className="flex flex-col gap-3 sm:flex-row sm:items-end">
				<div className="flex-1">
					<Input
						label={t("account.displayName")}
						hint={t("account.displayNameHint")}
						error={error}
						value={name}
						maxLength={100}
						autoComplete="nickname"
						onChange={(e) => {
							setName(e.target.value);
							setError(undefined);
						}}
					/>
				</div>
				<Button type="submit" disabled={busy || !changed} className="sm:mb-6">
					{t("account.save")}
				</Button>
			</form>
			{profile.ward && (
				<dl className="flex flex-col gap-1">
					<dt className="text-small font-medium text-ink">{t("account.ward")}</dt>
					<dd className="text-body">
						{profile.ward.name} · {profile.ward.constituency} · {profile.ward.county}
					</dd>
					<dd className="text-small text-muted">{t("account.wardNote")}</dd>
				</dl>
			)}
			<p className="text-small text-muted">
				{t("account.memberSince", { date: formatDate(profile.memberSince, lang) })}
			</p>
		</Section>
	);
}

/**
 * Profile photo: shown at once from the picked file, with progress over it
 * while it uploads and is processed (EXIF and GPS are removed), then used on
 * the user's posts, replies and the header.
 */
function ProfilePhoto({ profile, onSaved }: { profile: Profile; onSaved: (p: Profile) => void }) {
	const { t } = useT();
	const toast = useToast();
	const router = useRouter();
	const input = useRef<HTMLInputElement>(null);
	const hintId = useId();
	const [preview, setPreview] = useState<string>();
	const [progress, setProgress] = useState<UploadProgress | null>(null);
	const [removing, setRemoving] = useState(false);
	const [error, setError] = useState<string>();
	const busy = progress !== null || removing;

	useEffect(() => {
		if (!preview) return;
		return () => URL.revokeObjectURL(preview);
	}, [preview]);

	function done(next: Profile, message: MessageKey) {
		onSaved(next);
		toast(t(message));
		void router.invalidate(); // header, composer and new posts show it
	}

	async function choose(file: File | undefined) {
		if (!file) return;
		if (mediaKind(file.type) !== "image") return setError(t("account.photoType"));
		const bad = checkFile(file);
		if (bad) return setError(uploadFailureText(bad, t));
		setError(undefined);
		setPreview(URL.createObjectURL(file));
		setProgress({ stage: "uploading", fraction: 0 });
		const up = await uploadMedia(file, "", setProgress);
		const res = up.ok
			? await callApiEither((api) => api.account.setAvatar({ payload: { mediaId: up.media.mediaId } })).then(
					settle,
				)
			: null;
		setProgress(null);
		setPreview(undefined);
		if (!up.ok) return setError(uploadFailureText(up.failure, t));
		if (res && !res.ok) return setError(describeError(res.error, t));
		if (res) done(res.value, "account.photoSaved");
	}

	async function remove() {
		setRemoving(true);
		const res = await callApiEither((api) => api.account.removeAvatar()).then(settle);
		setRemoving(false);
		if (!res.ok) return setError(describeError(res.error, t));
		setError(undefined);
		done(res.value, "account.photoRemoved");
	}

	return (
		<div className="flex items-center gap-4">
			<div className="relative shrink-0">
				{preview ? (
					<span className="inline-flex h-24 w-24 overflow-hidden rounded-full bg-surface-2">
						<img src={preview} alt="" className="h-full w-full object-cover" />
					</span>
				) : (
					<Avatar name={profile.displayName || "?"} src={profile.avatar?.url} size="xl" />
				)}
				{progress && (
					<span className="absolute inset-0 flex items-center justify-center rounded-full bg-kenya-black/60 text-micro font-medium text-paper">
						<span role="status">
							{progress.stage === "uploading"
								? t("composer.uploading", { percent: Math.round(progress.fraction * 100) })
								: t("composer.processing", { kind: t("media.image") })}
						</span>
					</span>
				)}
			</div>
			<div className="flex min-w-0 flex-col gap-2">
				<span className="text-small font-medium text-ink">{t("account.photo")}</span>
				<div className="flex flex-wrap gap-2">
					<Button
						type="button"
						variant="secondary"
						size="sm"
						disabled={busy}
						aria-describedby={hintId}
						onClick={() => input.current?.click()}
					>
						{profile.avatar ? t("account.changePhoto") : t("account.addPhoto")}
					</Button>
					{profile.avatar && (
						<Button type="button" variant="ghost" size="sm" disabled={busy} onClick={remove}>
							{t("account.removePhoto")}
						</Button>
					)}
				</div>
				<p id={hintId} className="text-small text-muted">
					{t("account.photoHint")}
				</p>
				{error && (
					<p role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
			</div>
			<input
				ref={input}
				type="file"
				accept={MEDIA_TYPES.image.join(",")}
				className="sr-only"
				tabIndex={-1}
				aria-hidden="true"
				data-testid="avatar-input"
				onChange={(e) => {
					void choose(e.target.files?.[0]);
					e.target.value = "";
				}}
			/>
		</div>
	);
}

// ---- Language --------------------------------------------------------------------------

function LanguageSection({ profile, onSaved }: { profile: Profile; onSaved: (p: Profile) => void }) {
	const { t } = useT();
	const toast = useToast();
	const [error, setError] = useState<string>();
	return (
		<Section id="language" titleKey="account.language">
			<div className="flex flex-col gap-2">
				<span className="text-small font-medium text-ink">{t("account.appLanguage")}</span>
				<LanguageToggle />
			</div>
			<div className="max-w-xs">
				<Select
					label={t("account.smsLanguage")}
					value={profile.preferredLang}
					error={error}
					onChange={async (e) => {
						const preferredLang = e.target.value as "en" | "sw";
						const res = await saveProfile({ preferredLang });
						if (!res.ok) return setError(describeError(res.error, t));
						setError(undefined);
						onSaved(res.value);
						toast(t("account.saved"));
					}}
					options={[
						{ value: "sw", label: t("language.sw") },
						{ value: "en", label: t("language.en") },
					]}
				/>
			</div>
		</Section>
	);
}

// ---- Display ---------------------------------------------------------------------------

const THEMES: readonly Theme[] = ["system", "light", "dark"];

function DisplaySection() {
	const { t } = useT();
	// Rendered with the root's cookie-derived prefs (defaults if absent).
	const { display } = useRouteContext({ from: "__root__" }) as { display?: DisplayPrefs };
	const [prefs, setPrefs] = useState<DisplayPrefs>(display ?? DEFAULT_DISPLAY);
	const themeName = useId();
	const update = (change: Partial<DisplayPrefs>) => {
		const next = { ...prefs, ...change };
		setPrefs(next);
		saveDisplay(next);
	};
	return (
		<Section id="display" titleKey="account.display">
			<fieldset className="flex flex-col gap-2">
				<legend className="mb-2 text-small font-medium text-ink">{t("account.theme")}</legend>
				<div className="flex flex-wrap gap-2">
					{THEMES.map((theme) => (
						<label
							key={theme}
							className={cn(
								"inline-flex min-h-11 cursor-pointer items-center rounded-full border px-4 text-small",
								"has-focus-visible:ring-3 has-focus-visible:ring-accent",
								prefs.theme === theme ? "border-ink bg-surface-2 font-medium" : "border-border",
							)}
						>
							<input
								type="radio"
								name={themeName}
								className="sr-only"
								checked={prefs.theme === theme}
								onChange={() => update({ theme })}
							/>
							{t(`account.theme.${theme}`)}
						</label>
					))}
				</div>
			</fieldset>
			<Switch
				label={t("account.largeText")}
				hint={t("account.largeTextHint")}
				checked={prefs.largeText}
				onChange={(largeText) => update({ largeText })}
			/>
			<Switch
				label={t("account.highContrast")}
				hint={t("account.highContrastHint")}
				checked={prefs.highContrast}
				onChange={(highContrast) => update({ highContrast })}
			/>
			<Switch
				label={t("account.reducedMotion")}
				hint={t("account.reducedMotionHint")}
				checked={prefs.reducedMotion}
				onChange={(reducedMotion) => update({ reducedMotion })}
			/>
		</Section>
	);
}

// ---- Security --------------------------------------------------------------------------

function SecuritySection({ profile, onEnabled }: { profile: Profile; onEnabled: () => void }) {
	const { t } = useT();
	const logout = useLogout();
	const [enrolment, setEnrolment] = useState<{ secret: string; otpauthUri: string } | null>(null);
	const [code, setCode] = useState("");
	const [error, setError] = useState<string>();
	const [busy, setBusy] = useState(false);

	async function start() {
		setBusy(true);
		const res = settle(await callApiEither((api) => api.account.mfaEnrol()));
		setBusy(false);
		if (!res.ok) return setError(describeError(res.error, t));
		setError(undefined);
		setEnrolment(res.value);
	}

	async function verify(e: FormEvent) {
		e.preventDefault();
		if (!/^\d{6}$/.test(code)) return setError(t("account.mfaCodeInvalid"));
		setBusy(true);
		const res = settle(await callApiEither((api) => api.account.mfaActivate({ payload: { code } })));
		setBusy(false);
		if (!res.ok) return setError(describeError(res.error, t));
		setEnrolment(null);
		setError(undefined);
		onEnabled();
	}

	return (
		<Section id="security" titleKey="account.security">
			<div className="flex flex-col gap-3">
				<div className="flex flex-wrap items-center justify-between gap-3">
					<div className="flex flex-col">
						<span className="text-body text-ink">{t("account.mfa")}</span>
						<span className="text-small text-muted">{t("account.mfaHint")}</span>
					</div>
					{profile.mfaEnabled ? (
						<span className="rounded-full bg-kenya-green/10 px-3 py-1 text-small font-medium text-kenya-green">
							{t("account.mfaOn")}
						</span>
					) : (
						!enrolment && (
							<Button variant="secondary" onClick={() => void start()} disabled={busy}>
								{t("account.mfaSetup")}
							</Button>
						)
					)}
				</div>
				{profile.mfaEnabled && !enrolment && error === undefined && (
					<p role="status" className="sr-only">
						{t("account.mfaDone")}
					</p>
				)}
				{enrolment && (
					<form onSubmit={verify} noValidate className="flex flex-col gap-3 rounded-md bg-surface p-4">
						<p className="text-small">{t("account.mfaStep1")}</p>
						<code className="w-fit select-all break-all rounded-sm bg-paper px-3 py-2 font-mono text-body tracking-widest">
							{enrolment.secret.match(/.{1,4}/g)?.join(" ")}
						</code>
						<a
							href={enrolment.otpauthUri}
							className="w-fit text-small text-accent underline underline-offset-2"
						>
							{t("account.mfaOpen")}
						</a>
						<p className="text-small">{t("account.mfaStep2")}</p>
						<div className="flex flex-wrap items-end gap-3">
							<div className="w-40">
								<Input
									label={t("account.mfaCode")}
									inputMode="numeric"
									autoComplete="one-time-code"
									maxLength={6}
									value={code}
									error={error}
									onChange={(e) => {
										setCode(e.target.value.replace(/\D/g, ""));
										setError(undefined);
									}}
								/>
							</div>
							<Button type="submit" disabled={busy} className={error ? "mb-6" : ""}>
								{t("account.mfaVerify")}
							</Button>
						</div>
					</form>
				)}
				{error && !enrolment && (
					<p role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
			</div>
			<Button variant="secondary" className="w-fit" onClick={() => void logout()}>
				{t("account.logoutAll")}
			</Button>
		</Section>
	);
}

// ---- Privacy ---------------------------------------------------------------------------

function PrivacySection({ profile, onChange }: { profile: Profile; onChange: (p: Profile) => void }) {
	const { t, lang } = useT();
	const toast = useToast();
	const navigate = useNavigate();
	const reasonId = useId();
	const [dialog, setDialog] = useState<"withdraw" | "erase" | null>(null);
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string>();
	const [erased, setErased] = useState<{ completionBy: string; retained: readonly string[] } | null>(null);
	const { consent } = profile;

	async function withdraw() {
		setBusy(true);
		const res = settle(await callApiEither((api) => api.account.withdrawConsent()));
		setBusy(false);
		if (!res.ok) return setError(describeError(res.error, t));
		onChange({ ...profile, consent: { ...consent, active: false, withdrawnAt: res.value.effectiveAt } });
		setDialog(null);
		toast(t("account.withdrawn"));
	}

	async function erase() {
		setBusy(true);
		const res = settle(
			await callApiEither((api) =>
				api.account.requestErasure({ payload: reason.trim() ? { reason: reason.trim() } : {} }),
			),
		);
		setBusy(false);
		if (!res.ok) return setError(describeError(res.error, t));
		setDialog(null);
		setErased(res.value);
	}

	if (erased) {
		// Signed out everywhere: the page stays only to say what happens next.
		return (
			<Section id="privacy" titleKey="account.privacy">
				<div role="status" className="flex flex-col gap-2">
					<p className="text-body">
						{t("account.erasureDone", { date: formatDate(erased.completionBy, lang) })}
					</p>
					{erased.retained.length > 0 && (
						<>
							<p className="text-small font-medium">{t("account.erasureKept")}</p>
							<ul className="list-disc pl-5 text-small text-muted">
								{erased.retained.map((r) => (
									<li key={r}>{r}</li>
								))}
							</ul>
						</>
					)}
				</div>
				<Button className="w-fit" onClick={() => void navigate({ to: "/", reloadDocument: true })}>
					{t("account.erasureHome")}
				</Button>
			</Section>
		);
	}

	return (
		<Section id="privacy" titleKey="account.privacy">
			<div className="flex flex-col gap-2">
				<span className="text-body text-ink">{t("account.consent")}</span>
				<p className="text-small text-muted">
					{consent.active && consent.grantedAt
						? t("account.consentActive", {
								date: formatDate(consent.grantedAt, lang),
								version: consent.version,
							})
						: consent.withdrawnAt
							? t("account.consentWithdrawn", { date: formatDate(consent.withdrawnAt, lang) })
							: "—"}
				</p>
				{consent.active && (
					<Button variant="secondary" className="w-fit" onClick={() => setDialog("withdraw")}>
						{t("account.withdraw")}
					</Button>
				)}
			</div>
			<div className="flex flex-col gap-2 border-t border-border pt-4">
				<span className="text-body text-ink">{t("account.erasure")}</span>
				{profile.erasure ? (
					<p role="status" className="text-small text-muted">
						{t("account.erasurePending", {
							requested: formatDate(profile.erasure.requestedAt, lang),
							due: formatDate(profile.erasure.completionBy, lang),
						})}
					</p>
				) : (
					<>
						<p className="text-small text-muted">{t("account.erasureHint")}</p>
						<Button variant="danger" className="w-fit" onClick={() => setDialog("erase")}>
							{t("account.erasure")}
						</Button>
					</>
				)}
			</div>

			<Modal open={dialog === "withdraw"} onClose={() => setDialog(null)} title={t("account.withdrawTitle")}>
				<p>{t("account.withdrawBody")}</p>
				{error && (
					<p role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
				<div className="flex justify-end gap-3">
					<Button variant="ghost" onClick={() => setDialog(null)} disabled={busy}>
						{t("common.cancel")}
					</Button>
					<Button variant="danger" onClick={() => void withdraw()} disabled={busy}>
						{t("account.withdrawConfirm")}
					</Button>
				</div>
			</Modal>

			<Modal open={dialog === "erase"} onClose={() => setDialog(null)} title={t("account.erasureTitle")}>
				<p>{t("account.erasureBody")}</p>
				<div className="flex flex-col gap-2">
					<label htmlFor={reasonId} className="text-small font-medium text-ink">
						{t("account.erasureReason")} <span className="text-muted">({t("common.optional")})</span>
					</label>
					<textarea
						id={reasonId}
						rows={3}
						value={reason}
						onChange={(e) => setReason(e.target.value)}
						className="w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
					/>
				</div>
				{error && (
					<p role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
				<div className="flex justify-end gap-3">
					<Button variant="ghost" onClick={() => setDialog(null)} disabled={busy}>
						{t("common.cancel")}
					</Button>
					<Button variant="danger" onClick={() => void erase()} disabled={busy}>
						{t("account.erasureConfirm")}
					</Button>
				</div>
			</Modal>
		</Section>
	);
}
