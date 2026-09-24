import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { LanguageToggle } from "@/components/civic/LanguageToggle";
import { useT } from "@/lib/i18n/I18nProvider";
import { renderWithProviders } from "../render";

function Title() {
	const { t } = useT();
	return <h1>{t("app.name")}</h1>;
}

afterEach(() => {
	// biome-ignore lint/suspicious/noDocumentCookie: test cleanup
	document.cookie = "lang=; Max-Age=0; Path=/";
	localStorage.clear();
	document.documentElement.lang = "";
});

describe("LanguageToggle", () => {
	it("switches language, persists it and updates <html lang> (T-W1.1.3.3, T-W1.1.3.6)", async () => {
		await renderWithProviders(
			<>
				<LanguageToggle />
				<Title />
			</>,
			{ lang: "sw" },
		);
		expect(await screen.findByRole("heading")).toHaveTextContent("Jukwaa la Kiraia");
		expect(screen.getByRole("button", { name: "Kiswahili" })).toHaveAttribute("aria-pressed", "true");

		await userEvent.click(screen.getByRole("button", { name: "English" }));

		await waitFor(() => expect(screen.getByRole("heading")).toHaveTextContent("Civic Platform"));
		expect(screen.getByRole("button", { name: "English" })).toHaveAttribute("aria-pressed", "true");
		expect(document.cookie).toContain("lang=en");
		expect(localStorage.getItem("civic.lang")).toBe("en");
		expect(document.documentElement.lang).toBe("en");
		expect(screen.getByRole("status")).toHaveTextContent("Language changed to English");
	});

	it("labels each option in its own language", async () => {
		await renderWithProviders(<LanguageToggle />, { lang: "en" });
		expect(await screen.findByRole("button", { name: "Kiswahili" })).toHaveAttribute("lang", "sw");
		expect(screen.getByRole("button", { name: "English" })).toHaveAttribute("lang", "en");
	});
});
