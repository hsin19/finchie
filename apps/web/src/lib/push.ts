// Pushes to this device (internal/push on the backend): asking for them, stopping them, and keeping the
// backend told of this device's subscription, which a browser may renew and a push service may drop.

import {
    api,
    unwrap,
} from "#lib/api/client.js";

/** Where this device stands: its browser cannot take pushes, the owner blocked them, or they are off or on. */
export type PushState = "unsupported" | "denied" | "off" | "on";

/** Whether this browser can take pushes. On an iPhone, only Finchie opened from the home screen can. */
function pushSupported(): boolean {
    return typeof window !== "undefined" && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

/** A VAPID public key as the backend gives it, base64url, as the bytes PushManager takes. */
export function applicationServerKey(key: string): Uint8Array<ArrayBuffer> {
    const base64 = (key + "=".repeat((4 - (key.length % 4)) % 4)).replaceAll("-", "+").replaceAll("_", "/");
    return Uint8Array.from(atob(base64), c => c.charCodeAt(0));
}

async function subscription(): Promise<PushSubscription | null> {
    const registration = await navigator.serviceWorker.getRegistration();
    return registration ? registration.pushManager.getSubscription() : null;
}

export async function pushState(): Promise<PushState> {
    if (!pushSupported()) return "unsupported";
    if (Notification.permission === "denied") return "denied";
    return Notification.permission === "granted" && (await subscription()) ? "on" : "off";
}

async function send(s: PushSubscription): Promise<void> {
    const { endpoint, keys } = s.toJSON();
    if (!endpoint || !keys?.p256dh || !keys.auth) throw new Error("瀏覽器給的訂閱不完整");
    unwrap(await api().POST("/api/push/subscribe", { body: { endpoint, keys: { p256dh: keys.p256dh, auth: keys.auth } } }));
}

/** Asks for pushes on this device, from a click, as browsers require, and tells the backend. */
export async function turnOn(publicKey: string): Promise<PushState> {
    if ((await Notification.requestPermission()) !== "granted") return pushState();
    const registration = await navigator.serviceWorker.ready;
    const s = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: applicationServerKey(publicKey) });
    await send(s);
    return "on";
}

/** Stops pushes to this device. */
export async function turnOff(): Promise<PushState> {
    const s = await subscription();
    if (s) {
        unwrap(await api().POST("/api/push/unsubscribe", { body: { endpoint: s.endpoint } }));
        await s.unsubscribe();
    }
    return pushState();
}

/** Pushes a test to every device subscribed; how many took it. */
export async function testPush(): Promise<number> {
    return unwrap(await api().POST("/api/push/test")).sent;
}

/**
 * Tells the backend again of this device's subscription, when the app opens: the backend drops one its
 * push service says is gone, and a browser may have renewed it since.
 */
export async function renewPush(): Promise<void> {
    if (!pushSupported() || Notification.permission !== "granted") return;
    const s = await subscription();
    if (s) await send(s);
}
