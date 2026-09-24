import { useState } from "react";
import { Input } from "@/components/ui/Input";
import { EyeIcon, EyeOffIcon } from "@/components/ui/icons";
import { useT } from "@/lib/i18n/I18nProvider";
import { digitsOnly } from "@/lib/validation";

type Props = {
	value: string;
	onChange: (value: string) => void;
	error?: string | undefined;
	hint?: string | undefined;
};

/**
 * National ID field: masked by default, with an eye toggle inside the field
 * to view or hide what was typed. Numeric keypad, digits only, never
 * autofilled. The toggle keeps one accessible name and reports its state
 * with aria-pressed, so screen readers hear "Show national ID number,
 * toggle button, pressed/not pressed".
 */
export function NationalIdInput({ value, onChange, error, hint }: Props) {
	const { t } = useT();
	const [visible, setVisible] = useState(false);
	return (
		<Input
			label={t("join.identity.id")}
			hint={hint}
			error={error}
			type={visible ? "text" : "password"}
			inputMode="numeric"
			autoComplete="off"
			autoCorrect="off"
			spellCheck={false}
			maxLength={8}
			className="font-mono tracking-widest"
			value={value}
			onChange={(e) => onChange(digitsOnly(e.target.value, 8))}
			trailing={
				<button
					type="button"
					aria-label={t("join.identity.reveal")}
					aria-pressed={visible}
					onClick={() => setVisible((v) => !v)}
					// Keep the caret in the field when toggled with a pointer.
					onMouseDown={(e) => e.preventDefault()}
					className="inline-flex h-11 w-11 items-center justify-center rounded-sm text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
				>
					{visible ? <EyeOffIcon /> : <EyeIcon />}
				</button>
			}
		/>
	);
}
