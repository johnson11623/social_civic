import { useRouter } from "@tanstack/react-router";
import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";

import { LANG_COOKIE, LANG_COOKIE_MAX_AGE, LANG_STORAGE_KEY, type Lang } from "./lang";
import { type MessageKey, translate, type Vars } from "./messages";

type I18n = {
	lang: Lang;
	t: (key: MessageKey, vars?: Vars) => string;
	setLang: (lang: Lang) => void;
};

const I18nContext = createContext<I18n | null>(null);

/** Persist the choice where the server (cookie) and this browser (storage) can see it. */
function persist(lang: Lang) {
	// biome-ignore lint/suspicious/noDocumentCookie: plain cookie so SSR renders the chosen language
	document.cookie = `${LANG_COOKIE}=${lang}; Path=/; Max-Age=${LANG_COOKIE_MAX_AGE}; SameSite=Lax`;
	try {
		localStorage.setItem(LANG_STORAGE_KEY, lang);
	} catch {
		// Private mode or storage disabled: the cookie is enough.
	}
	document.documentElement.lang = lang;
}

export function I18nProvider({ initialLang, children }: { initialLang: Lang; children: ReactNode }) {
	const router = useRouter();
	const [lang, setLangState] = useState<Lang>(initialLang);

	const setLang = useCallback(
		(next: Lang) => {
			persist(next);
			setLangState(next);
			// Re-run loaders so server-localized data (e.g. API errors) follows.
			void router.invalidate();
		},
		[router],
	);

	const value = useMemo<I18n>(
		() => ({ lang, setLang, t: (key, vars) => translate(lang, key, vars) }),
		[lang, setLang],
	);
	return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

/** T-W1.1.3.2 — translate in components: `const { t } = useT()`. */
export function useT(): I18n {
	const ctx = useContext(I18nContext);
	if (!ctx) throw new Error("useT must be used inside <I18nProvider>");
	return ctx;
}
