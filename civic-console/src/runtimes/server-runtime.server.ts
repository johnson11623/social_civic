/**
 * T-W1.1.2.3 — server runtime. During SSR the ApiClient calls the handlers
 * directly (no HTTP); mountApi serves the same handlers to the browser at
 * /api/*. Both share the runtime's Backend instance.
 */
import { Layer, Logger, ManagedRuntime } from "effect";
import { makeSsrApiClientLayer, mountApi } from "effect-tanstack-start/server";

import { ApiContract } from "@/api/api-contract";
import { ApiImplLive } from "@/api/api-impl.server";
import { ApiClient } from "@/services/api-client-tag";
import { Backend } from "@/services/backend.server";

const SsrApiClientLive = makeSsrApiClientLayer(ApiContract, ApiImplLive, ApiClient);

const logger = process.env.NODE_ENV === "production" ? Logger.json : Logger.pretty;

export const serverRuntime = ManagedRuntime.make(
	SsrApiClientLive.pipe(Layer.provideMerge(Backend.Default), Layer.provideMerge(logger)),
);

export const apiHandler = mountApi(ApiContract, { serverRuntime, apiLayer: ApiImplLive });
