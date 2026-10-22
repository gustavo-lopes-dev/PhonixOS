// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	/** Versão do `package.json`, injetada pelo `define` de `vite.config.ts`. */
	const __APP_VERSION__: string;

	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};
