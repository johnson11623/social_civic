import { useNavigate, useRouter } from "@tanstack/react-router";
import { useCallback } from "react";

import { callApiPromise } from "@/runtimes/get-runtime";

/** T-W1.3.2.4 — end the session (cookies cleared by the BFF) and go to the landing page. */
export function useLogout() {
	const router = useRouter();
	const navigate = useNavigate();
	return useCallback(async () => {
		await callApiPromise((api) => api.auth.logout()).catch(() => undefined);
		await router.invalidate();
		await navigate({ to: "/" });
	}, [router, navigate]);
}
