import { resolve } from "$app/paths";
import { redirect } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

// Uploading moved to the document list.
export const load = (() => {
    redirect(308, resolve("/documents"));
}) satisfies PageLoad;
