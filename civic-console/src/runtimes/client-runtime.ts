/**
 * T-W1.1.2.4 — browser runtime: the ApiClient makes fetch requests to the
 * /api/* splat route on this origin.
 */
import { ManagedRuntime } from "effect";
import { makeHttpApiClientLayer } from "effect-tanstack-start/client";

import { ApiContract } from "@/api/api-contract";
import { ApiClient } from "@/services/api-client-tag";

export const clientRuntime = ManagedRuntime.make(makeHttpApiClientLayer(ApiContract, ApiClient));
