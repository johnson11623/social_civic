import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Channel, Media } from "@/api/api-contract";
import { renderWithProviders } from "../render";

// Posting with a photo or video (docs/media) against a mocked API client.

const api = { media: { createUpload: vi.fn(), complete: vi.fn(), get: vi.fn() } };
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { Composer } = await import("@/components/civic/Composer");
const { PostMedia } = await import("@/components/civic/PostMedia");
const { checkFile } = await import("@/lib/upload");

const general: Channel = {
	channelId: "c1",
	wardId: 551,
	name: "general",
	category: "general",
	readOnly: false,
	canPost: true,
};

const ready = (over: Partial<Media> = {}): Media => ({
	mediaId: "m1",
	kind: "image",
	state: "ready",
	altText: "A dry tap at the market",
	width: 1200,
	height: 800,
	placeholder: "data:image/webp;base64,AA",
	images: [
		{
			name: "thumbnail",
			width: 320,
			height: 213,
			webpUrl: "http://cdn/m1/thumbnail.webp",
			jpegUrl: "http://cdn/m1/thumbnail.jpg",
		},
		{
			name: "medium",
			width: 1080,
			height: 720,
			webpUrl: "http://cdn/m1/medium.webp",
			jpegUrl: "http://cdn/m1/medium.jpg",
		},
	],
	...over,
});

// A fake XMLHttpRequest that reports progress and succeeds.
class FakeXHR {
	static last: FakeXHR | undefined;
	upload: { onprogress?: (e: { lengthComputable: boolean; loaded: number; total: number }) => void } = {};
	status = 0;
	method = "";
	url = "";
	headers: Record<string, string> = {};
	onload?: () => void;
	onerror?: () => void;
	onabort?: () => void;
	open(method: string, url: string) {
		this.method = method;
		this.url = url;
	}
	setRequestHeader(k: string, v: string) {
		this.headers[k] = v;
	}
	send(body: File) {
		FakeXHR.last = this;
		this.upload.onprogress?.({ lengthComputable: true, loaded: body.size / 2, total: body.size });
		setTimeout(() => {
			this.status = 200;
			this.onload?.();
		}, 0);
	}
	abort() {
		this.onabort?.();
	}
}

beforeEach(() => {
	for (const fn of Object.values(api.media)) fn.mockReset();
	vi.stubGlobal("XMLHttpRequest", FakeXHR);
	vi.stubGlobal(
		"URL",
		Object.assign(URL, { createObjectURL: () => "blob:preview", revokeObjectURL: () => {} }),
	);
});
afterEach(() => vi.unstubAllGlobals());

const photo = (size = 2048, type = "image/png") => new File([new Uint8Array(size)], "tap.png", { type });

describe("checking files before upload", () => {
	it("refuses unsupported types and oversize files", () => {
		expect(checkFile(photo(10, "image/gif"))).toEqual({ reason: "type" });
		expect(checkFile(photo(11 * 1024 * 1024))).toEqual({ reason: "size", kind: "image" });
		expect(checkFile(new File([new Uint8Array(10)], "clip.mp4", { type: "video/mp4" }))).toBeNull();
	});
});

// The photo picker inside the dialog.
const fileInput = () =>
	screen.getByRole("dialog").querySelector('input[type="file"][accept^="image"]') as HTMLInputElement;

describe("composer with a photo", () => {
	async function open() {
		const onSubmit = vi.fn().mockResolvedValue(null);
		const user = userEvent.setup();
		await renderWithProviders(<Composer channels={[general]} onSubmit={onSubmit} />);
		await user.click(screen.getAllByRole("button", { name: "Start a post" })[0] as HTMLElement);
		await screen.findByRole("button", { name: "Post" }); // the form loads on first open
		return { user, onSubmit };
	}

	it("uploads straight to storage, waits for processing, then posts with the media", async () => {
		api.media.createUpload.mockReturnValue(
			Effect.succeed({
				mediaId: "m1",
				kind: "image",
				upload: {
					url: "http://s3/originals/m1",
					method: "PUT",
					headers: { "Content-Type": "image/png" },
					expiresAt: "x",
				},
			}),
		);
		api.media.complete.mockReturnValue(Effect.succeed(ready({ state: "processing", images: [] })));
		api.media.get.mockReturnValue(Effect.succeed(ready()));
		const { user, onSubmit } = await open();

		const dialog = screen.getByRole("dialog", { name: "Create a post" });
		expect(within(dialog).getByRole("button", { name: "Post" })).toBeDisabled();
		await user.upload(fileInput(), photo());
		// Shown large with a remove button; no description to fill in.
		expect(dialog.querySelector('img[src="blob:preview"]')).not.toBeNull();
		expect(within(dialog).getByRole("button", { name: "Remove" })).toBeInTheDocument();
		expect(within(dialog).queryByRole("textbox", { name: /Describe/ })).toBeNull();
		expect(within(dialog).queryByRole("button", { name: "Cancel" })).toBeNull();
		expect(api.media.createUpload).not.toHaveBeenCalled();

		await user.click(within(dialog).getByRole("button", { name: "Post" }));

		await waitFor(() => expect(onSubmit).toHaveBeenCalled());
		expect(api.media.createUpload).toHaveBeenCalledWith({
			payload: { mimeType: "image/png", sizeBytes: 2048, altText: "" },
		});
		expect(FakeXHR.last).toMatchObject({
			method: "PUT",
			url: "http://s3/originals/m1",
			headers: { "Content-Type": "image/png" },
		});
		expect(api.media.complete).toHaveBeenCalledWith({ path: { mediaId: "m1" } });
		// Text is optional with media; the ready media goes to the post.
		expect(onSubmit).toHaveBeenCalledWith(
			general,
			"",
			expect.objectContaining({ mediaId: "m1", state: "ready" }),
		);
	});

	it("refuses a file that's too big before uploading anything", async () => {
		const { user } = await open();
		await user.upload(fileInput(), photo(11 * 1024 * 1024));
		expect(screen.getByRole("alert")).toHaveTextContent("Photos can be up to 10 MB.");
		expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
	});

	it("removes the attached photo", async () => {
		const { user } = await open();
		await user.upload(fileInput(), photo());
		await user.click(screen.getByRole("button", { name: "Remove" }));
		expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
		expect(screen.getByRole("button", { name: "Post" })).toBeDisabled();
	});

	it("keeps the draft when processing fails", async () => {
		api.media.createUpload.mockReturnValue(
			Effect.succeed({
				mediaId: "m2",
				kind: "image",
				upload: { url: "http://s3/x", method: "PUT", headers: {}, expiresAt: "x" },
			}),
		);
		api.media.complete.mockReturnValue(
			Effect.succeed(ready({ mediaId: "m2", state: "failed", error: "not a usable image or video" })),
		);
		const { user, onSubmit } = await open();
		await user.upload(fileInput(), photo());
		await user.type(screen.getByRole("textbox", { name: "What do you want to talk about?" }), "Look at this");
		await user.click(screen.getByRole("button", { name: "Post" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("We couldn't use that file.");
		expect(onSubmit).not.toHaveBeenCalled();
		expect(screen.getByRole("textbox", { name: "What do you want to talk about?" })).toHaveValue(
			"Look at this",
		);
	});
});

describe("media on posts", () => {
	it("renders a responsive, lazy photo with its description and reserved size", async () => {
		const { container } = await renderWithProviders(<PostMedia media={ready()} />);
		const img = screen.getByRole("img", { name: "A dry tap at the market" });
		expect(img).toHaveAttribute("loading", "lazy");
		expect(img).toHaveAttribute("width", "1200");
		expect(img).toHaveAttribute("height", "800");
		expect(img.getAttribute("srcset")).toContain("http://cdn/m1/medium.jpg 1080w");
		expect(container.querySelector("source")?.getAttribute("srcset")).toContain(
			"http://cdn/m1/thumbnail.webp 320w",
		);
	});

	it("serves JPEG only when there is no WebP", async () => {
		const jpegOnly = ready({
			images: [{ name: "medium", width: 1080, height: 720, jpegUrl: "http://cdn/m1/medium.jpg" }],
		});
		const { container } = await renderWithProviders(<PostMedia media={jpegOnly} />);
		expect(container.querySelector("source")).toBeNull();
		expect(screen.getByRole("img")).toHaveAttribute("src", "http://cdn/m1/medium.jpg");
	});

	it("names an undescribed photo after who shared it", async () => {
		await renderWithProviders(<PostMedia media={ready({ altText: "" })} authorName="Amina" />);
		expect(screen.getByRole("img", { name: "Photo shared by Amina" })).toBeInTheDocument();
	});

	it("shows a video's poster and loads the player only on play", async () => {
		const user = userEvent.setup();
		const video = ready({
			kind: "video",
			images: [],
			width: 640,
			height: 360,
			hlsUrl: "http://cdn/m1/master.m3u8",
			poster: {
				name: "poster",
				width: 640,
				height: 360,
				webpUrl: "p.webp",
				jpegUrl: "http://cdn/m1/poster.jpg",
			},
		});
		await renderWithProviders(<PostMedia media={video} />);
		const el = document.querySelector("video") as HTMLVideoElement;
		expect(el).toHaveAttribute("poster", "http://cdn/m1/poster.jpg");
		expect(el).toHaveAttribute("preload", "none");
		expect(el.getAttribute("src")).toBeNull();
		// Native HLS (Safari): the stream is attached on play.
		el.canPlayType = () => "maybe";
		el.play = () => Promise.resolve();
		await user.click(screen.getByRole("button", { name: "Play video: A dry tap at the market" }));
		expect(el.getAttribute("src")).toBe("http://cdn/m1/master.m3u8");
		expect(within(document.body).queryByRole("button", { name: /Play video/ })).toBeNull();
	});
});
