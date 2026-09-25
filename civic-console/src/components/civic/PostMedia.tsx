import { useEffect, useRef, useState } from "react";

import type { Media } from "@/api/api-contract";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * An attached photo or video on a post (docs/media). Space is reserved
 * from the known dimensions, so nothing shifts as it loads; the blurred
 * placeholder shows until then; images are lazy and pick the right size.
 */
export function PostMedia({ media, authorName }: { media: Media; authorName?: string | undefined }) {
	const { t } = useT();
	// Without a description, say what it is and whose it is (never an empty alt).
	const label =
		media.altText ||
		(authorName
			? t(media.kind === "video" ? "media.videoBy" : "media.photoBy", { name: authorName })
			: t(`media.${media.kind}`));
	return media.kind === "video" ? (
		<VideoPlayer media={media} label={label} />
	) : (
		<Picture media={media} label={label} />
	);
}

/** width/height attributes let the browser reserve the box before loading. */
const box = (m: Media) => ({ width: m.width ?? 16, height: m.height ?? 9 });

function Picture({ media, label }: { media: Media; label: string }) {
	const images = media.images ?? [];
	const fallback = images.find((i) => i.name === "medium") ?? images[0];
	if (!fallback) return null;
	const { width, height } = box(media);
	const sizes = "(min-width: 768px) 640px, 100vw";
	return (
		<picture>
			<source
				type="image/webp"
				srcSet={images.map((i) => `${i.webpUrl} ${i.width}w`).join(", ")}
				sizes={sizes}
			/>
			<img
				src={fallback.jpegUrl}
				srcSet={images.map((i) => `${i.jpegUrl} ${i.width}w`).join(", ")}
				sizes={sizes}
				width={width}
				height={height}
				alt={label}
				loading="lazy"
				decoding="async"
				className="h-auto w-full rounded-md bg-surface-2 object-cover"
				style={
					media.placeholder
						? { backgroundImage: `url(${media.placeholder})`, backgroundSize: "cover" }
						: undefined
				}
			/>
		</picture>
	);
}

/** Minimal surface of hls.js used here. */
type Hls = { loadSource(url: string): void; attachMedia(el: HTMLMediaElement): void; destroy(): void };

/**
 * HLS video. Safari plays HLS natively; elsewhere hls.js is loaded the
 * first time someone presses play, so it never weighs on page load.
 */
function VideoPlayer({ media, label }: { media: Media; label: string }) {
	const { t } = useT();
	const video = useRef<HTMLVideoElement>(null);
	const hls = useRef<Hls | null>(null);
	const [started, setStarted] = useState(false);
	const { width, height } = box(media);
	const poster = media.poster?.jpegUrl;

	useEffect(() => () => hls.current?.destroy(), []);

	async function start() {
		const el = video.current;
		if (!el || !media.hlsUrl) return;
		setStarted(true);
		if (el.canPlayType("application/vnd.apple.mpegurl")) {
			el.src = media.hlsUrl;
		} else {
			const { default: HlsJs } = await import("hls.js");
			if (!HlsJs.isSupported()) return;
			const player = new HlsJs() as unknown as Hls;
			player.loadSource(media.hlsUrl);
			player.attachMedia(el);
			hls.current = player;
		}
		void el.play().catch(() => undefined);
	}

	return (
		<div className="relative overflow-hidden rounded-md bg-kenya-black">
			{/* biome-ignore lint/a11y/useMediaCaption: user uploads have no caption track yet; the description is the label */}
			<video
				ref={video}
				width={width}
				height={height}
				poster={poster}
				controls={started}
				playsInline
				preload="none"
				aria-label={label}
				className="h-auto w-full"
			/>
			{!started && (
				<button
					type="button"
					onClick={() => void start()}
					aria-label={t("media.play", { alt: label })}
					className="absolute inset-0 flex items-center justify-center bg-kenya-black/20 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-inset focus-visible:ring-accent"
				>
					<span
						aria-hidden="true"
						className="flex h-16 w-16 items-center justify-center rounded-full bg-paper/90 text-h1 text-ink"
					>
						▶
					</span>
				</button>
			)}
		</div>
	);
}
