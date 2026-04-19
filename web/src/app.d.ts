// See https://kit.svelte.dev/docs/types#app
declare global {
	namespace App {
		/** Shown on `+error.svelte` via `$page.error` */
		interface Error {
			message: string;
		}
		// interface Locals {}
		// interface PageData {}
		// interface Platform {}
	}
}

export {};
