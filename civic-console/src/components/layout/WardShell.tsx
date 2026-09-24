import { useNavigate, useRouter } from "@tanstack/react-router";
import { lazy, type ReactNode, Suspense, useState } from "react";

import type { Channel, ChannelList, Level } from "@/api/api-contract";
import { ChannelSidebar } from "@/components/civic/ChannelSidebar";
import { Drawer } from "@/components/ui/Drawer";
import { useToast } from "@/components/ui/Toast";
import { describeError, type Settled } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";

// Loaded on first use: not needed for the first paint (3G budget).
const CreateChannelModal = lazy(() =>
	import("@/components/civic/CreateChannelModal").then((m) => ({ default: m.CreateChannelModal })),
);

type Props = {
	channels: Settled<ChannelList>;
	activeChannelId?: string | undefined;
	activeLevel?: Level | undefined;
	children: ReactNode;
};

/**
 * Template: the signed-in ward layout. Desktop (lg+): channel sidebar beside
 * the content. Below lg: a "Channels" button opens the same sidebar as a
 * drawer (T-W2.1.1.4). Creating a channel adds it and opens it (T-W2.1.2.5).
 */
export function WardShell({ channels, activeChannelId, activeLevel, children }: Props) {
	const { t } = useT();
	const toast = useToast();
	const router = useRouter();
	const navigate = useNavigate();
	const [drawer, setDrawer] = useState(false);
	const [creating, setCreating] = useState(false);

	const onCreated = async (channel: Channel) => {
		setCreating(false);
		setDrawer(false);
		toast(t("createChannel.created", { name: channel.name }));
		await navigate({ to: "/channels/$channelId", params: { channelId: channel.channelId } });
		await router.invalidate(); // refresh the channel list everywhere
	};

	const wardName = channels.ok && channels.value.ward ? channels.value.ward.name : t("sidebar.open");

	const sidebar = (inDrawer: boolean) =>
		channels.ok ? (
			<ChannelSidebar
				channels={channels.value}
				activeChannelId={activeChannelId}
				activeLevel={activeLevel}
				onCreate={() => setCreating(true)}
				onNavigate={inDrawer ? () => setDrawer(false) : undefined}
			/>
		) : (
			<p role="alert" className="p-4 text-small text-danger">
				{describeError(channels.error, t)}
			</p>
		);

	return (
		<div className="mx-auto flex max-w-7xl gap-6 px-4 lg:px-6">
			<aside className="sticky top-0 hidden max-h-dvh w-72 shrink-0 self-start overflow-y-auto border-r border-border bg-surface lg:block">
				{sidebar(false)}
			</aside>
			<div className="min-w-0 flex-1">
				<button
					type="button"
					onClick={() => setDrawer(true)}
					// Contains the visible text (label-in-name), plus what the button opens.
					aria-label={`${wardName} — ${t("sidebar.channels")}`}
					className="mt-4 inline-flex min-h-11 items-center gap-2 rounded-sm border border-border px-3 text-small font-medium text-ink hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent lg:hidden"
				>
					<span aria-hidden="true">☰</span>
					{wardName}
				</button>
				{children}
			</div>
			<Drawer open={drawer} onClose={() => setDrawer(false)} title={t("sidebar.channels")}>
				{drawer && sidebar(true)}
			</Drawer>
			{creating && (
				<Suspense>
					<CreateChannelModal open onClose={() => setCreating(false)} onCreated={onCreated} />
				</Suspense>
			)}
		</div>
	);
}
