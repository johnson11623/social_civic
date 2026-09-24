/**
 * T-W1.1.2.4 — browser runtime: the ApiClient makes fetch requests to the
 * /api/* splat route on this origin.
 */
import { HttpClient, HttpClientRequest } from "@effect/platform";
import { ManagedRuntime } from "effect";
import { makeHttpApiClientLayer } from "effect-tanstack-start/client";

import { ApiContract } from "@/api/api-contract";
import { ApiClient } from "@/services/api-client-tag";

// T-W1.1.3.4 — every call carries the page's language as Accept-Language
// (the BFF also honours the `lang` cookie, which takes precedence).
const withLanguage = HttpClient.mapRequest((request: HttpClientRequest.HttpClientRequest) =>
	HttpClientRequest.setHeader(request, "accept-language", document.documentElement.lang || "sw"),
);

export const clientRuntime = ManagedRuntime.make(
	makeHttpApiClientLayer(ApiContract, ApiClient, { transformClient: withLanguage }),
);
