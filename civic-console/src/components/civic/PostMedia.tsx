import { type CSSProperties, type ReactNode, useEffect, useRef, useState } from "react";

import type { Media } from "@/api/api-contract";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * An attached photo or video on a post (docs/media), shown the way large
 * feeds do it:
 * - the frame's shape is kept between 4:5 (portrait) and 1.91:1 (landscape),
 *   so a tall phone video doesn't fill the screen; what doesn't fit sits over
 *   a blurred copy of itself instead of being cropped;
 * - space is reserved from the known size and the blurred placeholder shows
 *   at once, the real picture fading in over it (no layout shift);
 * - videos play muted while on screen and pause when scrolled away, one at a
 *   time, unless the viewer saves data, is on 2G or prefers reduced motion.
 */
export function PostMedia({
	media,
	authorName,
	priority = false,
}: {
	media: Media;
	authorName?: string | undefined;
	/** Load now, at high priority (the first photo on the page), instead of lazily. */
	priority?: boolean;
}) {
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
		<Picture media={media} label={label} priority={priority} />
	);
}

// ---- Frame -------------------------------------------------------------------------------

/** Feed frames stay between these shapes (width / height), as on Instagram and LinkedIn. */
const TALLEST = 4 / 5;
const WIDEST = 1.91;

export function frameRatio(width = 16, height = 9): number {
	const r = width / Math.max(height, 1);
	return Math.min(WIDEST, Math.max(TALLEST, r));
}

/**
 * The box a photo or video sits in. When the media's own shape is outside the
 * frame's, the blurred placeholder fills the bars.
 */
function Frame({
	media,
	backdrop,
	children,
}: {
	media: Media;
	backdrop?: string | undefined;
	children: ReactNode;
}) {
	const ratio = frameRatio(media.width, media.height);
	return (
		// Height is also capped (about half a laptop screen, 65% of a phone's),
		// so one post never takes over the feed; the rest shows as bars.
		<div
			className="relative max-h-media w-full overflow-hidden rounded-md bg-surface-2"
			style={{ aspectRatio: String(ratio) } as CSSProperties}
		>
			{backdrop && (
				<img
					src={backdrop}
					alt=""
					aria-hidden="true"
					className="absolute inset-0 h-full w-full scale-110 object-cover opacity-60 blur-2xl"
				/>
			)}
			{children}
		</div>
	);
}

// ---- Photos ------------------------------------------------------------------------------

function Picture({ media, label, priority }: { media: Media; label: string; priority: boolean }) {
	const img = useRef<HTMLImageElement>(null);
	const [loaded, setLoaded] = useState(false);
	const images = media.images ?? [];
	const fallback = images.find((i) => i.name === "medium") ?? images[0];

	// A cached image can finish before hydration attaches onLoad.
	useEffect(() => {
		if (img.current?.complete) setLoaded(true);
	}, []);

	if (!fallback) return null;
	// The feed column is at most ~640px wide; phones use the full width.
	const sizes = "(min-width: 768px) 640px, 100vw";
	return (
		<Frame media={media} backdrop={media.placeholder}>
			{media.placeholder && !loaded && (
				<img
					src={media.placeholder}
					alt=""
					aria-hidden="true"
					className="absolute inset-0 h-full w-full object-contain"
				/>
			)}
			<picture>
				{/* Only when every size has WebP: the local worker may produce JPEG only. */}
				{images.every((i) => i.webpUrl) && (
					<source
						type="image/webp"
						srcSet={images.map((i) => `${i.webpUrl} ${i.width}w`).join(", ")}
						sizes={sizes}
					/>
				)}
				<img
					ref={img}
					src={fallback.jpegUrl}
					srcSet={images.map((i) => `${i.jpegUrl} ${i.width}w`).join(", ")}
					sizes={sizes}
					width={media.width}
					height={media.height}
					alt={label}
					loading={priority ? "eager" : "lazy"}
					fetchPriority={priority ? "high" : "auto"}
					decoding="async"
					onLoad={() => setLoaded(true)}
					className={cn(
						"absolute inset-0 h-full w-full object-contain motion-safe:transition-opacity motion-safe:duration-300",
						loaded ? "opacity-100" : "opacity-0",
					)}
				/>
			</picture>
		</Frame>
	);
}

// ---- Video -------------------------------------------------------------------------------

/** Minimal surface of hls.js used here. */
type Hls = { loadSource(url: string): void; attachMedia(el: HTMLMediaElement): void; destroy(): void };

type NetworkInfo = { saveData?: boolean; effectiveType?: string };

/** Muted autoplay is skipped for data savers, 2G and reduced motion. */
export function canAutoplay(): boolean {
	if (typeof window === "undefined" || typeof IntersectionObserver === "undefined") return false;
	if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return false;
	if (document.documentElement.dataset.motion === "reduce") return false; // account setting
	const net = (navigator as Navigator & { connection?: NetworkInfo }).connection;
	return !(net?.saveData || /(^|-)2g$/.test(net?.effectiveType ?? ""));
}

/** Only one video plays at a time across the feed. */
let playing: HTMLVideoElement | null = null;
function claim(el: HTMLVideoElement) {
	if (playing && playing !== el) playing.pause();
	playing = el;
}

const clock = (s: number) => {
	const n = Math.max(0, Math.ceil(s));
	return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, "0")}`;
};

/**
 * HLS video. Safari plays HLS natively; elsewhere hls.js is fetched shortly
 * before the video scrolls into view (or on the first press of play), and
 * starts at the lowest rendition so playback begins at once.
 */
function VideoPlayer({ media, label }: { media: Media; label: string }) {
	const { t } = useT();
	const wrap = useRef<HTMLDivElement>(null);
	const video = useRef<HTMLVideoElement>(null);
	const hls = useRef<Hls | null>(null);
	const attaching = useRef<Promise<void> | null>(null);
	const userPaused = useRef(false);
	const [auto] = useState(canAutoplay);
	const [started, setStarted] = useState(false); // something has played
	const [paused, setPaused] = useState(true);
	const [muted, setMuted] = useState(true);
	const [controls, setControls] = useState(false); // native controls, after a tap
	const [left, setLeft] = useState(media.durationMs ? media.durationMs / 1000 : 0);
	const [progress, setProgress] = useState(0);
	const poster = media.poster?.jpegUrl;

	function attach(): Promise<void> {
		const el = video.current;
		if (!el || !media.hlsUrl) return Promise.resolve();
		attaching.current ??= (async () => {
			if (el.canPlayType("application/vnd.apple.mpegurl")) {
				el.src = media.hlsUrl as string;
				return;
			}
			const { default: HlsJs } = await import("hls.js");
			if (!HlsJs.isSupported()) return;
			// Lowest rendition first, never above the player's size.
			const player = new HlsJs({ startLevel: 0, capLevelToPlayerSize: true, maxBufferLength: 15 });
			player.loadSource(media.hlsUrl as string);
			player.attachMedia(el);
			hls.current = player as unknown as Hls;
		})();
		return attaching.current;
	}

	async function play(opts: { withSound?: boolean } = {}) {
		const el = video.current;
		if (!el) return;
		await attach();
		if (opts.withSound) {
			el.muted = false;
			setMuted(false);
		}
		claim(el);
		setStarted(true);
		await el.play().catch(() => undefined);
	}

	useEffect(() => {
		// React doesn't reliably reflect `muted`; autoplay needs the property set.
		if (video.current) video.current.muted = true;
		return () => hls.current?.destroy();
	}, []);

	// Autoplay: fetch the player a screen ahead, play at 60% visible, pause when leaving.
	// biome-ignore lint/correctness/useExhaustiveDependencies: observers are set up once per video
	useEffect(() => {
		const box = wrap.current;
		if (!auto || !box) return;
		const ahead = new IntersectionObserver(
			([e]) => {
				if (e?.isIntersecting) {
					void attach();
					ahead.disconnect();
				}
			},
			{ rootMargin: "600px 0px" },
		);
		const seen = new IntersectionObserver(
			([e]) => {
				const el = video.current;
				if (!e || !el) return;
				if (e.intersectionRatio >= 0.6) {
					if (!userPaused.current) void play();
				} else if (!el.paused && !document.fullscreenElement) {
					el.pause();
				}
			},
			{ threshold: [0, 0.6] },
		);
		ahead.observe(box);
		seen.observe(box);
		return () => {
			ahead.disconnect();
			seen.disconnect();
		};
	}, [auto]);

	function togglePause() {
		const el = video.current;
		if (!el) return;
		if (el.paused) {
			userPaused.current = false;
			void play();
		} else {
			userPaused.current = true;
			el.pause();
		}
	}

	function toggleMute() {
		const el = video.current;
		if (!el) return;
		el.muted = !el.muted;
		setMuted(el.muted);
		if (!el.muted) claim(el);
	}

	// Tapping the picture opens it up, as on LinkedIn: sound on, full controls.
	function open() {
		setControls(true);
		void play({ withSound: true });
	}

	const chip =
		"inline-flex h-9 min-w-9 items-center justify-center rounded-full bg-black/70 px-2 text-micro font-medium text-white hover:bg-black focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent";

	return (
		<div ref={wrap}>
			<Frame media={media} backdrop={media.placeholder ?? poster}>
				<video
					ref={video}
					width={media.width}
					height={media.height}
					poster={poster}
					controls={controls}
					muted={muted}
					loop={!controls}
					playsInline
					preload="none"
					aria-label={label}
					onPlay={() => setPaused(false)}
					onPause={() => setPaused(true)}
					onTimeUpdate={(e) => {
						const el = e.currentTarget;
						if (!el.duration) return;
						setLeft(el.duration - el.currentTime);
						setProgress(el.currentTime / el.duration);
					}}
					className="absolute inset-0 h-full w-full object-contain"
				/>
				{!controls &&
					(started ? (
						<>
							{/* The picture itself: tap for sound and full controls. */}
							<button
								type="button"
								onClick={open}
								aria-label={t("media.open", { alt: label })}
								className="absolute inset-0 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-inset focus-visible:ring-accent"
							/>
							<div className="absolute inset-x-2 bottom-3 flex items-center justify-between gap-2">
								<button
									type="button"
									onClick={togglePause}
									aria-label={paused ? t("media.resume") : t("media.pause")}
									className={chip}
								>
									<span aria-hidden="true">{paused ? "▶" : "❚❚"}</span>
								</button>
								<span className="flex items-center gap-2">
									{left > 0 && (
										<span className="rounded-full bg-black/70 px-2 py-1 text-micro tabular-nums text-white">
											{clock(left)}
										</span>
									)}
									<button
										type="button"
										onClick={toggleMute}
										aria-label={muted ? t("media.unmute") : t("media.mute")}
										aria-pressed={!muted}
										className={chip}
									>
										<SpeakerIcon muted={muted} />
									</button>
								</span>
							</div>
							<span
								aria-hidden="true"
								className="absolute inset-x-0 bottom-0 h-1 origin-left bg-kenya-green"
								style={{ transform: `scaleX(${progress})` }}
							/>
						</>
					) : (
						<button
							type="button"
							onClick={() => {
								setControls(true);
								void play({ withSound: true });
							}}
							aria-label={t("media.play", { alt: label })}
							className="absolute inset-0 flex items-center justify-center bg-black/20 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-inset focus-visible:ring-accent"
						>
							<span
								aria-hidden="true"
								className="flex h-16 w-16 items-center justify-center rounded-full bg-white/90 text-h1 text-black"
							>
								▶
							</span>
							{left > 0 && (
								<span className="absolute right-2 bottom-3 rounded-full bg-black/70 px-2 py-1 text-micro tabular-nums text-white">
									{clock(left)}
								</span>
							)}
						</button>
					))}
			</Frame>
		</div>
	);
}

function SpeakerIcon({ muted }: { muted: boolean }) {
	return (
		<svg
			viewBox="0 0 24 24"
			aria-hidden="true"
			className="h-5 w-5"
			fill="none"
			stroke="currentColor"
			strokeWidth="2"
		>
			<path d="M11 5 6 9H3v6h3l5 4V5Z" fill="currentColor" />
			{muted ? (
				<path d="m16 9 5 6m0-6-5 6" strokeLinecap="round" />
			) : (
				<path d="M16 8.5a5 5 0 0 1 0 7M18.5 6a8.5 8.5 0 0 1 0 12" strokeLinecap="round" />
			)}
		</svg>
	);
}
