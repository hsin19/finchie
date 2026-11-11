// Signing in is Cloudflare Access's job, at the edge (ADR 0008), and its session ends after a month.
// From then on Access answers every request with a redirect to its login page, which only a page load
// can follow. Installed to a home screen, the app stays open for weeks and has no reload button, so it
// notices by itself: API requests do not follow redirects (`redirect: "manual"`, client.ts), which
// shows one as what it is instead of the network error a refused cross-origin redirect looks like.

const SIGNED_OUT = "登入過期了：重新整理會重新登入";

/** No session: Access redirecting to its login page, or the backend refusing the request (ADR 0008). */
export function signedOut(response: Response): boolean {
    return response.type === "opaqueredirect" || response.status === 401;
}

/**
 * A fetch for API requests that signs in again once the session is gone. `signIn` reloads the page
 * and says whether it did.
 *
 * Only a read signs in again: it has nothing to lose, and its answer never comes, so no error shows
 * on the page that is leaving. A change reports the failure instead, since it may come from a draft a
 * reload would drop; the next read reloads anyway.
 */
export function withSession(fetchFn: typeof fetch, signIn: () => boolean): typeof fetch {
    return async (input, init) => {
        const response = await fetchFn(input, init);
        if (!signedOut(response)) return response;
        const method = input instanceof Request ? input.method : init?.method ?? "GET";
        if (method === "GET" && signIn()) return new Promise<Response>(() => {});
        // A redirect not followed has no status and no body: say what it means.
        return response.status === 401 ? response : Response.json({ message: SIGNED_OUT }, { status: 401 });
    };
}

const RELOADED_AT = "finchie:signed-in-again";
/** Long enough for Access and Google to sign in, short next to how often a session ends. */
const RELOAD_AGAIN_AFTER_MS = 60_000;

/**
 * What reloads the page so that Access signs in: once for all the requests that find the session gone
 * together, and not again on the page that reload gives. Still signed out there means the reload
 * does not help (the backend refuses the token), and the error is shown rather than reloading forever.
 */
export function reloader({
    now = () => Date.now(),
    storage = (): Pick<Storage, "getItem" | "setItem"> => sessionStorage,
    reload = () => location.reload(),
} = {}): () => boolean {
    let leaving = false;
    return () => {
        if (leaving) return true;
        try {
            const store = storage();
            // A stamp in the future (the clock moved back) counts as old.
            const age = now() - Number(store.getItem(RELOADED_AT));
            if (age >= 0 && age < RELOAD_AGAIN_AFTER_MS) return false;
            store.setItem(RELOADED_AT, String(now()));
        } catch {
            // Nowhere to remember the reload: without that it could repeat forever.
            return false;
        }
        leaving = true;
        reload();
        return true;
    };
}
