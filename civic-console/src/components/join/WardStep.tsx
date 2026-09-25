import { useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Spinner } from "@/components/ui/Spinner";
import { type Area, type Boundaries, loadBoundaries, searchWards, type WardHit } from "@/lib/boundaries";
import { useT } from "@/lib/i18n/I18nProvider";

export type WardChoice = { code: number; name: string; constituency: string; county: string };

type Codes = { county: string; constituency: string; ward: string };

const byCode = <T extends Area>(nodes: ReadonlyArray<T> | undefined, code: string) =>
	nodes?.find((n) => String(n.code) === code);

/**
 * T-W1.3.1.5 — find your ward by search (typo-tolerant, from the IEBC data)
 * or step by step: County → Constituency → Ward. The data ships with the app
 * (lib/boundaries, prefetched when the join page opens), so results appear
 * as you type, with no request per keystroke.
 */
export function WardStep({
	initial,
	onBack,
	onNext,
}: {
	initial: WardChoice | undefined;
	onBack: () => void;
	onNext: (ward: WardChoice) => void;
}) {
	const { t } = useT();
	const [tree, setTree] = useState<Boundaries | undefined>();
	const [loadError, setLoadError] = useState<string | undefined>();
	const [attempt, setAttempt] = useState(0);
	const [codes, setCodes] = useState<Codes>({ county: "", constituency: "", ward: "" });
	const [query, setQuery] = useState("");
	const [error, setError] = useState(false);
	const initialised = useRef(false);

	// biome-ignore lint/correctness/useExhaustiveDependencies: attempt retries the load
	useEffect(() => {
		let live = true;
		setLoadError(undefined);
		loadBoundaries().then(
			(b) => live && setTree(b),
			() => live && setLoadError(t("join.ward.loadFailed")),
		);
		return () => {
			live = false;
		};
	}, [t, attempt]);

	// Restore a previous choice once the tree is available.
	useEffect(() => {
		if (!tree || !initial || initialised.current) return;
		initialised.current = true;
		for (const county of tree.counties) {
			for (const constituency of county.children ?? []) {
				if (constituency.children?.some((w) => w.code === initial.code)) {
					setCodes({
						county: String(county.code),
						constituency: String(constituency.code),
						ward: String(initial.code),
					});
				}
			}
		}
	}, [tree, initial]);

	// Instant: ~1,450 wards scored in well under a millisecond.
	const results = useMemo<ReadonlyArray<WardHit> | undefined>(
		() => (tree && query.trim().length >= 2 ? searchWards(tree, query) : undefined),
		[tree, query],
	);

	const county = byCode(tree?.counties, codes.county);
	const constituency = byCode(county?.children, codes.constituency);
	const ward = byCode(constituency?.children, codes.ward);
	const options = (nodes: ReadonlyArray<Area> | undefined) =>
		(nodes ?? []).map((n) => ({ value: String(n.code), label: n.name }));

	const choice = useMemo<WardChoice | undefined>(
		() =>
			ward && constituency && county
				? { code: ward.code, name: ward.name, constituency: constituency.name, county: county.name }
				: undefined,
		[ward, constituency, county],
	);

	const pick = (r: WardHit) => {
		setCodes({
			county: String(r.county.code),
			constituency: String(r.constituency.code),
			ward: String(r.code),
		});
		setQuery("");
		setError(false);
	};

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(e) => {
				e.preventDefault();
				if (!choice) return setError(true);
				onNext(choice);
			}}
		>
			<h2 className="text-h1">{t("join.ward.heading")}</h2>

			<div className="flex flex-col gap-2">
				<Input
					label={t("join.ward.search")}
					hint={t("join.ward.searchHint")}
					type="search"
					autoComplete="off"
					value={query}
					onChange={(e) => setQuery(e.target.value)}
				/>
				{results && (
					<ul
						aria-live="polite"
						className="flex flex-col divide-y divide-border rounded-md border border-border"
					>
						{results.length === 0 && (
							<li className="p-3 text-small text-muted">{t("join.ward.noResults")}</li>
						)}
						{results.map((r) => (
							<li key={r.code}>
								<button
									type="button"
									onClick={() => pick(r)}
									className="flex min-h-11 w-full items-center px-3 py-2 text-left text-body hover:bg-surface-2"
								>
									{r.label}
								</button>
							</li>
						))}
					</ul>
				)}
			</div>

			<fieldset className="flex flex-col gap-4">
				<legend className="mb-2 text-small font-medium text-muted">{t("join.ward.or")}</legend>
				{!tree && !loadError && <Spinner label={t("join.ward.loading")} />}
				{loadError && (
					<div role="alert" className="flex flex-col items-start gap-2">
						<p className="text-small text-danger">{loadError}</p>
						<Button type="button" variant="secondary" size="sm" onClick={() => setAttempt((n) => n + 1)}>
							{t("common.retry")}
						</Button>
					</div>
				)}
				{tree && (
					<>
						<Select
							label={t("join.ward.county")}
							placeholder={t("join.ward.choose")}
							options={options(tree.counties)}
							value={codes.county}
							onChange={(e) => setCodes({ county: e.target.value, constituency: "", ward: "" })}
						/>
						<Select
							label={t("join.ward.constituency")}
							placeholder={t("join.ward.choose")}
							options={options(county?.children)}
							value={codes.constituency}
							disabled={!county}
							onChange={(e) => setCodes({ ...codes, constituency: e.target.value, ward: "" })}
						/>
						<Select
							label={t("join.ward.ward")}
							placeholder={t("join.ward.choose")}
							options={options(constituency?.children)}
							value={codes.ward}
							disabled={!constituency}
							error={error && !choice ? t("join.ward.required") : undefined}
							onChange={(e) => {
								setCodes({ ...codes, ward: e.target.value });
								setError(false);
							}}
						/>
					</>
				)}
			</fieldset>

			{choice && (
				<p className="rounded-md bg-elevated p-3 text-body" aria-live="polite">
					{t("join.ward.selected", { ward: `${choice.name}, ${choice.constituency}, ${choice.county}` })}
				</p>
			)}

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
