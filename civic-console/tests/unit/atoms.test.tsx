import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Avatar, initials } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { LevelIndicator } from "@/components/ui/LevelIndicator";
import { Spinner } from "@/components/ui/Spinner";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

describe("Button (T-W1.2.2.1)", () => {
	it("renders every variant and size with token classes", () => {
		for (const variant of ["primary", "secondary", "ghost", "danger"] as const) {
			for (const size of ["sm", "md", "lg"] as const) {
				const { unmount } = render(
					<Button variant={variant} size={size}>
						x
					</Button>,
				);
				const el = screen.getByRole("button");
				expect(el.className).toMatch(size === "sm" ? /min-h-9/ : size === "md" ? /min-h-11/ : /min-h-13/);
				expect(el.className).toContain("focus-visible:ring-3");
				unmount();
			}
		}
	});

	it("meets the 44px touch target by default and never submits forms by accident", () => {
		render(<Button>Post</Button>);
		const el = screen.getByRole("button", { name: "Post" });
		expect(el).toHaveClass("min-h-11", "bg-accent", "text-on-accent");
		expect(el).toHaveAttribute("type", "button");
	});

	it("is operable by keyboard and respects disabled", async () => {
		const onClick = vi.fn();
		render(
			<>
				<Button onClick={onClick}>Like</Button>
				<Button disabled onClick={onClick}>
					Off
				</Button>
			</>,
		);
		await userEvent.tab();
		expect(screen.getByRole("button", { name: "Like" })).toHaveFocus();
		await userEvent.keyboard("{Enter}");
		await userEvent.keyboard(" ");
		expect(onClick).toHaveBeenCalledTimes(2);
		await userEvent.click(screen.getByRole("button", { name: "Off" }));
		expect(onClick).toHaveBeenCalledTimes(2);
	});

	it("lets className override variant classes", () => {
		render(<Button className="bg-danger">x</Button>);
		const el = screen.getByRole("button");
		expect(el).toHaveClass("bg-danger");
		expect(el).not.toHaveClass("bg-accent");
	});

	it("passes axe", async () => {
		const { container } = render(<Button full>Jiunge</Button>);
		await expectNoA11yViolations(container);
	});
});

describe("Input (T-W1.2.2.2)", () => {
	it("links the label, hint and error; announces the error", () => {
		render(<Input label="National ID" hint="8 digits" error="National ID must be 8 digits." />);
		const input = screen.getByLabelText("National ID");
		expect(input).toHaveAttribute("aria-invalid", "true");
		expect(input).toHaveAccessibleDescription("8 digits National ID must be 8 digits.");
		expect(screen.getByRole("alert")).toHaveTextContent("National ID must be 8 digits.");
		expect(input).toHaveClass("border-danger");
	});

	it("has no error state by default and unique ids", () => {
		render(
			<>
				<Input label="Phone" />
				<Input label="Phone" />
			</>,
		);
		const [a, b] = screen.getAllByLabelText("Phone");
		expect(a?.id).not.toBe(b?.id);
		expect(a).not.toHaveAttribute("aria-invalid");
		expect(a).not.toHaveAttribute("aria-describedby");
		expect(screen.queryByRole("alert")).toBeNull();
	});

	it("accepts typing", async () => {
		render(<Input label="Display name" />);
		await userEvent.type(screen.getByLabelText("Display name"), "Wanjiku");
		expect(screen.getByLabelText("Display name")).toHaveValue("Wanjiku");
	});

	it("passes axe with and without an error", async () => {
		const { container } = render(
			<>
				<Input label="Ward" />
				<Input label="OTP" error="Wrong code" hint="6 digits" />
			</>,
		);
		await expectNoA11yViolations(container);
	});
});

describe("Badge (T-W1.2.2.3)", () => {
	it("renders all variants with their token colours", () => {
		const cases = {
			default: "bg-surface-2",
			ward: "text-kenya-green",
			constituency: "text-accent",
			county: "text-warning",
			national: "text-kenya-red",
			verified: "text-verified",
			sponsored: "bg-sponsored",
			danger: "text-danger",
		} as const;
		for (const [variant, cls] of Object.entries(cases)) {
			const { unmount } = render(<Badge variant={variant as keyof typeof cases}>{variant}</Badge>);
			expect(screen.getByText(variant)).toHaveClass(cls);
			unmount();
		}
	});
});

describe("Avatar (T-W1.2.2.4)", () => {
	it("falls back to initials with an accessible name", () => {
		render(<Avatar name="Wanjiku Mwangi" />);
		expect(screen.getByRole("img", { name: "Wanjiku Mwangi" })).toHaveTextContent("WM");
	});

	it("uses the image with alt text", () => {
		render(<Avatar name="Brian" src="/brian.jpg" size="lg" />);
		const img = screen.getByRole("img", { name: "Brian" });
		expect(img).toHaveAttribute("src", "/brian.jpg");
		expect(img).toHaveAttribute("loading", "lazy");
	});

	it("computes initials robustly", () => {
		expect(initials("  amina   k  ")).toBe("AK");
		expect(initials("Peter")).toBe("P");
		expect(initials("Ñandú Ñu Otro")).toBe("ÑÑ");
		expect(initials("")).toBe("");
	});

	it("passes axe", async () => {
		const { container } = render(
			<>
				<Avatar name="Grace" />
				<Avatar name="Joseph" src="/j.png" />
			</>,
		);
		await expectNoA11yViolations(container);
	});
});

describe("Spinner (T-W1.2.2.5)", () => {
	it("is announced as a localized status", async () => {
		await renderWithProviders(<Spinner />, { lang: "sw" });
		expect(await screen.findByRole("status")).toHaveTextContent("Inapakia");
	});

	it("accepts a custom label", async () => {
		await renderWithProviders(<Spinner label="Posting" />);
		expect(await screen.findByRole("status")).toHaveTextContent("Posting");
	});
});

describe("LevelIndicator (T-W1.2.2.6)", () => {
	it("fills one dot per level and announces the level", async () => {
		const cases = [
			["ward", 1, "Level: Ward"],
			["constituency", 2, "Level: Constituency"],
			["county", 3, "Level: County"],
			["national", 4, "Level: National"],
		] as const;
		for (const [level, filled, label] of cases) {
			const { unmount } = await renderWithProviders(<LevelIndicator current={level} />);
			const el = await screen.findByRole("img", { name: label });
			const dots = el.querySelectorAll("span[aria-hidden]");
			expect(dots).toHaveLength(4);
			expect([...dots].filter((d) => d.classList.contains("bg-kenya-green"))).toHaveLength(filled);
			unmount();
		}
	});

	it("is localized", async () => {
		await renderWithProviders(<LevelIndicator current="constituency" />, { lang: "sw" });
		expect(await screen.findByRole("img", { name: "Ngazi: Eneo Bunge" })).toBeInTheDocument();
	});

	it("passes axe", async () => {
		const { container } = await renderWithProviders(<LevelIndicator current="county" />);
		await screen.findByRole("img");
		await expectNoA11yViolations(container);
	});
});
