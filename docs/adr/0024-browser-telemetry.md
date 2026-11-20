# ADR 0024: Browser telemetry

## Decision

The web app reports on itself with Grafana Faro, to whatever Faro collector the deployment names. ADR 0011 made the backend's side observable; the browser's stayed dark: a page that failed, a slow page on the phone, an error a deferred load caught and showed in place. With Faro, sessions, their pages, errors, and Web Vitals arrive beside the backend's telemetry, and a request's span starts the backend's trace: an upload in the browser, the API call, the parse job linked to it, and the parser, in one trace.

| | |
|---|---|
| Switch | `BACKEND_FARO_URL`, handed to the web app by `GET /api/settings` (`faroUrl`), like the upload limit: one image serves every deployment (ADR 0007), and the collector is the deployment's. Unset (local development, tests), the app downloads nothing more |
| Loading | The SDK is imported once the settings are in, into chunks of its own (some 70 KB compressed), so the first page never waits for it, and a failure to load it leaves the app as it is |
| SDK | `@grafana/faro-web-sdk` with its default instrumentations (errors and unhandled rejections, console warnings and errors, Web Vitals, sessions, fetch and XHR timings), and `@grafana/faro-web-tracing`. No session replay |
| Version | `app.version` is the release (`$app/env`), as the backend's `service.version` is |
| Traces | Requests to the app's own origin carry `traceparent`, so the backend's span, when its telemetry is on (ADR 0011), is a child of the browser's. Only the app's origin: the collector gets none |
| Errors | Besides what reaches `window.onerror`: what SvelteKit catches (`handleError` in `hooks.client.ts`; not a framework error, which is an address with no route) and what a deferred load catches to show in place (`Settled`) |
| Pages | Named by route (`/statements/[id]`), as the backend names its spans, not by each statement's address |

**What stays in the browser.** `scrub`, Faro's `beforeSend`, runs on everything before it leaves:

- Every URL it finds, the page's, a span's URL attributes, resource timings, words in a message, loses its query and fragment, and a span's `url.query` attribute is dropped: they hold what the owner searched for and filtered by (every filter is in the URL).
- The browser goes as its name, version, system, and whether it is a phone. ADR 0011 keeps the browser out of the backend's logs, where it tells nothing about a request; here, telling the phone from the desktop is much of the point. The full user agent string and its brands stay in the browser.

Where it all goes is the deployment's choice of collector. Grafana Cloud's Frontend Observability, the one in use, also sees the browser's address. A deployment that wants no third party, as ADR 0007 wanted for fonts, names a receiver of its own instead.

## Alternatives considered

- **The deployment's own receiver on the app's origin** (a path routed to Grafana Alloy's `faro.receiver`). No third party, and the browser's address stays home; but Grafana Cloud's Frontend Observability reads only what its own collector takes, so its views would be built by hand. Not a decision for the app: only another `BACKEND_FARO_URL`.
- **The backend relaying to the collector.** The same origin and the backend's own Access check, but the backend would carry, trace, and log its own telemetry's traffic.
- **Sentry.** Strong on errors, but a system of its own: its traces do not join the backend's.
- **OpenTelemetry's browser SDK alone.** The same tracing (Faro's is built on it), with sessions, Web Vitals, and error capture to assemble by hand.
- **The collector's URL at build time** (a `PUBLIC_` variable). It would tie the image to one deployment.
