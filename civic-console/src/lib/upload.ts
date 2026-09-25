/**
 * Media upload (docs/media): ask the API for a signed URL, PUT the file
 * straight to storage (it never passes through our servers), complete it,
 * then wait while the worker makes the variants.
 */
import type { Media } from "@/api/api-contract";
import { type ApiFailure, describeError, settle } from "@/lib/api-errors";
import { MAX_IMAGE_BYTES, MAX_VIDEO_BYTES, MEDIA_TYPES } from "@/lib/limits";
import { callApiEither } from "@/runtimes/get-runtime";

export type MediaKind = "image" | "video";

export type UploadProgress = { stage: "uploading"; fraction: number } | { stage: "processing" };

export type UploadFailure =
	| { reason: "type" }
	| { reason: "size"; kind: MediaKind }
	| { reason: "network" }
	| { reason: "processing" }
	| { reason: "api"; error: ApiFailure };

/** The kind of an acceptable file, or undefined. */
export function mediaKind(type: string): MediaKind | undefined {
	if ((MEDIA_TYPES.image as readonly string[]).includes(type)) return "image";
	if ((MEDIA_TYPES.video as readonly string[]).includes(type)) return "video";
	return undefined;
}

/** Checked in the browser first, so nothing is sent for a file we'd refuse. */
export function checkFile(file: File): UploadFailure | null {
	const kind = mediaKind(file.type);
	if (!kind) return { reason: "type" };
	if (file.size > (kind === "image" ? MAX_IMAGE_BYTES : MAX_VIDEO_BYTES)) return { reason: "size", kind };
	return null;
}

/** What to tell the user about a failed upload. */
export function uploadFailureText(f: UploadFailure, t: Parameters<typeof describeError>[1]): string {
	switch (f.reason) {
		case "type":
			return t("composer.badType");
		case "size":
			return f.kind === "image" ? t("composer.tooBigImage") : t("composer.tooBigVideo");
		case "network":
			return t("composer.uploadFailed");
		case "processing":
			return t("composer.processFailed");
		default:
			return describeError(f.error, t);
	}
}

/** PUT with progress events (fetch can't report upload progress). */
function put(
	url: string,
	file: File,
	headers: Record<string, string>,
	onProgress: (f: number) => void,
	signal?: AbortSignal,
) {
	return new Promise<boolean>((resolve) => {
		const xhr = new XMLHttpRequest();
		xhr.open("PUT", url);
		for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
		xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
		xhr.onload = () => resolve(xhr.status >= 200 && xhr.status < 300);
		xhr.onerror = () => resolve(false);
		xhr.onabort = () => resolve(false);
		signal?.addEventListener("abort", () => xhr.abort(), { once: true });
		xhr.send(file);
	});
}

const sleep = (ms: number, signal?: AbortSignal) =>
	new Promise<void>((resolve) => {
		const id = setTimeout(resolve, ms);
		signal?.addEventListener("abort", () => {
			clearTimeout(id);
			resolve();
		});
	});

export type UploadResult = { ok: true; media: Media } | { ok: false; failure: UploadFailure };

/** Upload and wait until ready. Polls with backoff for up to about two minutes. */
export async function uploadMedia(
	file: File,
	altText: string,
	onProgress: (p: UploadProgress) => void,
	signal?: AbortSignal,
): Promise<UploadResult> {
	const bad = checkFile(file);
	if (bad) return { ok: false, failure: bad };

	const ticket = settle(
		await callApiEither((api) =>
			api.media.createUpload({ payload: { mimeType: file.type, sizeBytes: file.size, altText } }),
		),
	);
	if (!ticket.ok) return { ok: false, failure: { reason: "api", error: ticket.error } };

	onProgress({ stage: "uploading", fraction: 0 });
	const { url, headers } = ticket.value.upload;
	if (!(await put(url, file, headers, (fraction) => onProgress({ stage: "uploading", fraction }), signal))) {
		return { ok: false, failure: { reason: "network" } };
	}

	onProgress({ stage: "processing" });
	const mediaId = ticket.value.mediaId;
	let res = settle(await callApiEither((api) => api.media.complete({ path: { mediaId } })));
	for (
		let wait = 500, spent = 0;
		res.ok && res.value.state === "processing" && spent < 120_000;
		spent += wait
	) {
		await sleep(wait, signal);
		if (signal?.aborted) return { ok: false, failure: { reason: "network" } };
		wait = Math.min(wait * 1.5, 4000);
		res = settle(await callApiEither((api) => api.media.get({ path: { mediaId } })));
	}
	if (!res.ok) return { ok: false, failure: { reason: "api", error: res.error } };
	if (res.value.state !== "ready") return { ok: false, failure: { reason: "processing" } };
	return { ok: true, media: res.value };
}
