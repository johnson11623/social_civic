import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { NationalIdInput } from "@/components/civic/NationalIdInput";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

function IdField() {
	const [value, setValue] = useState("");
	return <NationalIdInput value={value} onChange={setValue} hint="8 digits" />;
}

describe("NationalIdInput", () => {
	it("is masked by default and the eye toggles view/hide, keeping the value", async () => {
		await renderWithProviders(<IdField />, { lang: "en" });
		const input = await screen.findByLabelText("National ID number");
		const toggle = screen.getByRole("button", { name: "Show national ID number" });

		// Masked with CSS on a text field: no browser offer to save a password.
		expect(input).toHaveAttribute("type", "text");
		expect(input).toHaveAttribute("data-masked", "true");
		expect(toggle).toHaveAttribute("aria-pressed", "false");

		await userEvent.type(input, "12a34 5678");
		expect(input).toHaveValue("12345678");

		await userEvent.click(toggle);
		expect(input).not.toHaveAttribute("data-masked");
		expect(toggle).toHaveAttribute("aria-pressed", "true");
		expect(input).toHaveValue("12345678");

		await userEvent.click(toggle);
		expect(input).toHaveAttribute("data-masked", "true");
	});

	it("works from the keyboard and keeps a 44px target", async () => {
		await renderWithProviders(<IdField />, { lang: "en" });
		const input = await screen.findByLabelText("National ID number");
		await userEvent.click(input);
		await userEvent.tab();
		const toggle = screen.getByRole("button", { name: "Show national ID number" });
		expect(toggle).toHaveFocus();
		expect(toggle).toHaveClass("h-11", "w-11");
		await userEvent.keyboard("{Enter}");
		expect(input).toHaveAttribute("type", "text");
	});

	it("is localized", async () => {
		await renderWithProviders(<IdField />, { lang: "sw" });
		expect(
			await screen.findByRole("button", { name: "Onyesha nambari ya kitambulisho" }),
		).toBeInTheDocument();
	});

	it("passes axe", async () => {
		const { container } = await renderWithProviders(<IdField />, { lang: "en" });
		await screen.findByLabelText("National ID number");
		await expectNoA11yViolations(container);
	});
});

describe("Input trailing slot", () => {
	it("reserves room so text never runs under the control", () => {
		render(<Input label="With" trailing={<button type="button">x</button>} />);
		render(<Input label="Without" />);
		expect(screen.getByLabelText("With")).toHaveClass("pr-12");
		expect(screen.getByLabelText("Without")).not.toHaveClass("pr-12");
	});
});

describe("Select", () => {
	const options = [
		{ value: "22", label: "Kiambu" },
		{ value: "47", label: "Nairobi City" },
	];

	it("replaces the native arrow with an inset chevron and room for it", () => {
		const { container } = render(<Select label="County" placeholder="Choose…" options={options} />);
		const select = screen.getByLabelText("County");
		expect(select).toHaveClass("appearance-none", "pr-12", "pl-3");
		const chevron = container.querySelector("svg");
		expect(chevron).toHaveAttribute("aria-hidden", "true");
		expect(chevron?.parentElement).toHaveClass("pointer-events-none", "right-0", "w-11");
	});

	it("still works as a native select", async () => {
		render(<Select label="County" placeholder="Choose…" options={options} />);
		await userEvent.selectOptions(screen.getByLabelText("County"), "47");
		expect(screen.getByLabelText("County")).toHaveValue("47");
	});

	it("passes axe, enabled and disabled", async () => {
		const { container } = render(
			<>
				<Select label="County" placeholder="Choose…" options={options} />
				<Select label="Ward" placeholder="Choose…" options={[]} disabled error="Choose your ward" />
			</>,
		);
		await expectNoA11yViolations(container);
	});
});
