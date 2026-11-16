// Only for pushes: it shows what the backend pushes (internal/push) and opens /notifications when one is
// tapped. It keeps nothing and answers no request, so the app is always the one the backend serves.

import { today } from "#lib/dates.js";
import {
    type PushMessage,
    pushText,
} from "#lib/notices.js";
import { self as sw } from "$app/service-worker";

// Nothing it holds goes stale, so a new one takes over at once.
sw.addEventListener("install", () => void sw.skipWaiting());
sw.addEventListener("activate", event => event.waitUntil(sw.clients.claim()));

sw.addEventListener("push", event => {
    const message = (event.data?.json() ?? { base: "TWD" }) as PushMessage;
    const { title, body, tag } = pushText(message, today());
    event.waitUntil(sw.registration.showNotification(title, {
        body,
        tag,
        icon: new URL("pwa-192x192.png", sw.registration.scope).href,
        data: { url: new URL("notifications", sw.registration.scope).href },
    }));
});

sw.addEventListener("notificationclick", event => {
    event.notification.close();
    const url = (event.notification.data as { url?: string; } | null)?.url ?? sw.registration.scope;
    event.waitUntil((async () => {
        const [open] = await sw.clients.matchAll({ type: "window", includeUncontrolled: true });
        if (!open) {
            await sw.clients.openWindow(url);
            return;
        }
        await open.focus();
        // A window this worker does not control cannot be sent elsewhere; it shows the app, at least.
        await open.navigate(url).catch(() => undefined);
    })());
});
