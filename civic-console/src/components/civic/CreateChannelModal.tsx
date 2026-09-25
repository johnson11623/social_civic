import { type FormEvent, useId, useState } from "react";

import type { Channel, ChannelCategory } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { describeError, fieldErrors, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { CHANNEL_NAME, MAX_CHANNEL_DESCRIPTION } from "@/lib/limits";
import { callApiEither } from "@/runtimes/get-runtime";
import { categoryDot } from "./ChannelItem";

export const CATEGORIES: readonly ChannelCategory[] = [
	"general",
	"services",
	"opportunities",
	"safety",
	"culture",
];

type Props = {
	open: boolean;
	onClose: () => void;
	onCreated: (channel: Channel) => void;
};

/** "Water Points " → "water-points": what people type → what the rule allows. */
export const normalizeChannelName = (raw: string) => raw.trim().toLowerCase().replace(/\s+/g, "-");

export const validChannelName = (name: string) =>
	name.length >= 2 && name.length <= 40 && CHANNEL_NAME.test(name);

/**
 * W2.1.2 — create a channel in the caller's ward: name (validated as the
 * platform does, T-W2.1.2.2), category with its dot (T-W2.1.2.3), optional
 * description, and who can post (T-W2.1.2.4).
 */
export function CreateChannelModal({ open, onClose, onCreated }: Props) {
	const { t } = useT();
	return (
		<Modal open={open} onClose={onClose} title={t("createChannel.title")} fullScreenOnMobile>
			{open && <CreateChannelForm onCreated={onCreated} />}
		</Modal>
	);
}

function CreateChannelForm({ onCreated }: { onCreated: Props["onCreated"] }) {
	const { t } = useT();
	const ids = { category: useId(), visibility: useId(), description: useId() };
	const [name, setName] = useState("");
	const [category, setCategory] = useState<ChannelCategory>("general");
	const [description, setDescription] = useState("");
	const [readOnly, setReadOnly] = useState(false);
	const [errors, setErrors] = useState<{
		name?: string | undefined;
		description?: string | undefined;
		form?: string | undefined;
	}>({});
	const [busy, setBusy] = useState(false);

	const normalized = normalizeChannelName(name);
	const descriptionLength = Array.from(description.trim()).length;

	async function submit(e: FormEvent) {
		e.preventDefault();
		const next: typeof errors = {};
		if (!validChannelName(normalized)) next.name = t("createChannel.nameInvalid");
		if (descriptionLength > MAX_CHANNEL_DESCRIPTION)
			next.description = t("createChannel.descriptionTooLong", { max: MAX_CHANNEL_DESCRIPTION });
		setErrors(next);
		if (next.name || next.description) return;

		setBusy(true);
		const res = settle(
			await callApiEither((api) =>
				api.posts.createChannel({
					payload: {
						name: normalized,
						category,
						readOnly,
						...(description.trim() ? { description: description.trim() } : {}),
					},
				}),
			),
		);
		setBusy(false);
		if (res.ok) return onCreated(res.value);
		const fields = fieldErrors(res.error);
		if (res.error.code === "name_taken") setErrors({ name: t("createChannel.nameTaken") });
		else if (fields.name === "reserved") setErrors({ name: t("createChannel.nameReserved") });
		else if (fields.name) setErrors({ name: t("createChannel.nameInvalid") });
		else if (fields.description)
			setErrors({ description: t("createChannel.descriptionTooLong", { max: MAX_CHANNEL_DESCRIPTION }) });
		else setErrors({ form: describeError(res.error, t) });
	}

	return (
		<form onSubmit={submit} noValidate className="flex flex-col gap-5">
			<Input
				label={t("createChannel.name")}
				hint={t("createChannel.nameHint")}
				error={errors.name}
				value={name}
				maxLength={60}
				autoCapitalize="none"
				autoComplete="off"
				spellCheck={false}
				onChange={(e) => {
					setName(e.target.value);
					if (errors.name) setErrors((x) => ({ ...x, name: undefined }));
				}}
				onBlur={() => setName(normalized)}
				trailing={<span className="px-3 text-muted">#</span>}
			/>

			<fieldset className="flex flex-col gap-2">
				<legend id={ids.category} className="mb-2 text-small font-medium text-ink">
					{t("createChannel.category")}
				</legend>
				<div className="flex flex-wrap gap-2">
					{CATEGORIES.map((c) => (
						<label
							key={c}
							className={cn(
								"inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-full border px-3 text-small",
								"has-focus-visible:ring-3 has-focus-visible:ring-accent",
								category === c ? "border-ink bg-surface-2 font-medium" : "border-border",
							)}
						>
							<input
								type="radio"
								name={ids.category}
								value={c}
								checked={category === c}
								onChange={() => setCategory(c)}
								className="sr-only"
							/>
							<span aria-hidden="true" className={cn("h-2 w-2 rounded-full", categoryDot[c])} />
							{t(`category.${c}`)}
						</label>
					))}
				</div>
			</fieldset>

			<div className="flex flex-col gap-2">
				<label htmlFor={ids.description} className="text-small font-medium text-ink">
					{t("createChannel.description")} <span className="text-muted">({t("common.optional")})</span>
				</label>
				<textarea
					id={ids.description}
					rows={2}
					value={description}
					onChange={(e) => {
						setDescription(e.target.value);
						if (errors.description) setErrors((x) => ({ ...x, description: undefined }));
					}}
					aria-invalid={errors.description ? true : undefined}
					aria-describedby={`${ids.description}-count${errors.description ? ` ${ids.description}-error` : ""}`}
					className={cn(
						"w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink",
						"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
						errors.description && "border-danger",
					)}
				/>
				<p
					id={`${ids.description}-count`}
					className={cn(
						"self-end text-micro tabular-nums",
						descriptionLength > MAX_CHANNEL_DESCRIPTION ? "text-danger" : "text-muted",
					)}
				>
					<span aria-hidden="true">
						{t("composer.counter", { count: descriptionLength, max: MAX_CHANNEL_DESCRIPTION })}
					</span>
					<span className="sr-only">
						{t("composer.counterLabel", { remaining: MAX_CHANNEL_DESCRIPTION - descriptionLength })}
					</span>
				</p>
				{errors.description && (
					<p id={`${ids.description}-error`} role="alert" className="text-micro text-danger">
						{errors.description}
					</p>
				)}
			</div>

			<fieldset className="flex flex-col gap-2">
				<legend className="mb-2 text-small font-medium text-ink">{t("createChannel.visibility")}</legend>
				{[
					{ value: false, label: t("createChannel.public") },
					{ value: true, label: t("createChannel.readOnly") },
				].map((o) => (
					<label key={String(o.value)} className="flex min-h-11 cursor-pointer items-center gap-3 text-body">
						<input
							type="radio"
							name={ids.visibility}
							checked={readOnly === o.value}
							onChange={() => setReadOnly(o.value)}
							className="h-5 w-5 accent-kenya-green focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
						/>
						{o.label}
					</label>
				))}
			</fieldset>

			{errors.form && (
				<p role="alert" className="text-small text-danger">
					{errors.form}
				</p>
			)}
			<Button type="submit" disabled={busy} className="self-end">
				{t("createChannel.submit")}
			</Button>
		</form>
	);
}
