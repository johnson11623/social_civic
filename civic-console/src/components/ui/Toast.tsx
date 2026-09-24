import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";

import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";

type Tone = "success" | "error";
type ToastItem = { id: number; message: string; tone: Tone };
type Show = (message: string, tone?: Tone) => void;

const ToastContext = createContext<Show | null>(null);

/** How long a toast stays up (Web App Design §6.2: auto-dismiss 5s). */
export const TOAST_MS = 5000;

/**
 * Primitive: Toast — confirmations and errors announced through one polite
 * live region, dismissed after 5s or by hand.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
	const { t } = useT();
	const [items, setItems] = useState<ToastItem[]>([]);
	const next = useRef(0);
	const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

	const dismiss = useCallback((id: number) => {
		clearTimeout(timers.current.get(id));
		timers.current.delete(id);
		setItems((all) => all.filter((i) => i.id !== id));
	}, []);

	const show = useCallback<Show>(
		(message, tone = "success") => {
			const id = ++next.current;
			setItems((all) => [...all.slice(-2), { id, message, tone }]);
			timers.current.set(
				id,
				setTimeout(() => dismiss(id), TOAST_MS),
			);
		},
		[dismiss],
	);

	useEffect(() => {
		const pending = timers.current;
		return () => pending.forEach(clearTimeout);
	}, []);

	return (
		<ToastContext.Provider value={show}>
			{children}
			<div
				role="status"
				aria-live="polite"
				className="pointer-events-none fixed inset-x-4 bottom-24 z-50 flex flex-col items-center gap-2 md:bottom-6"
			>
				{items.map((item) => (
					<div
						key={item.id}
						className={cn(
							"pointer-events-auto flex w-full max-w-md items-center gap-3 rounded-md px-4 py-3 text-small shadow-lg",
							item.tone === "error" ? "bg-danger text-on-accent" : "bg-ink text-paper",
						)}
					>
						<span className="flex-1">{item.message}</span>
						<button
							type="button"
							onClick={() => dismiss(item.id)}
							className="-my-2 -mr-2 inline-flex min-h-11 min-w-11 items-center justify-center rounded-sm focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
						>
							<span aria-hidden="true">×</span>
							<span className="sr-only">{t("common.dismiss")}</span>
						</button>
					</div>
				))}
			</div>
		</ToastContext.Provider>
	);
}

/** Show a toast. Outside a ToastProvider (isolated tests) it does nothing. */
export function useToast(): Show {
	const show = useContext(ToastContext);
	return useMemo(() => show ?? (() => {}), [show]);
}
