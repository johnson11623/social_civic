/**
 * T-W1.1.2.2 — the one typed API client tag shared by the server and client
 * runtimes. Must stay client-safe: it imports only the contract.
 */
import { makeApiClientTag } from "effect-tanstack-start/client";

import { ApiContract } from "@/api/api-contract";

export const ApiClient = makeApiClientTag(ApiContract);
