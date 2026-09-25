import type { ReasonCode } from "@/api/api-contract";
import { Select } from "@/components/ui/Select";
import { useT } from "@/lib/i18n/I18nProvider";
import { REASONS } from "@/lib/moderation";

type Props = {
	label: string;
	value: ReasonCode | "";
	onChange: (reason: ReasonCode) => void;
	error?: string | undefined;
};

/** The fixed, harm-based reason list in the user's language (T-W2.2.2.1). */
export function ReasonSelect({ label, value, onChange, error }: Props) {
	const { t } = useT();
	return (
		<Select
			label={label}
			value={value}
			error={error}
			placeholder={t("common.choose")}
			onChange={(e) => onChange(e.target.value as ReasonCode)}
			options={REASONS.map((r) => ({ value: r, label: t(`reason.${r}`) }))}
		/>
	);
}
