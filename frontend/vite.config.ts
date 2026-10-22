import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';
import pkg from './package.json' with { type: 'json' };

// Backend Go local (porta canônica 8080) para validar as telas no `vite dev`.
const backend = 'http://localhost:8080';

export default defineConfig({
	plugins: [
		tailwindcss(),
		// SvelteKit 3: a configuração do Kit e do Svelte vive aqui (sem `svelte.config.js`).
		sveltekit({
			preprocess: vitePreprocess(),
			compilerOptions: {
				runes: true
			},
			// SPA estática: a rota raiz é prerenderizada em `index.html` (3.6/3.7) e as
			// demais navegações caem no fallback `200.html`, distinto do `index.html`.
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: '200.html',
				precompress: false,
				strict: true
			}),
			serviceWorker: {
				register: true
			}
		})
	],
	define: {
		__APP_VERSION__: JSON.stringify(pkg.version)
	},
	server: {
		proxy: {
			'/api': { target: backend, changeOrigin: true },
			'/ws': { target: backend, changeOrigin: true, ws: true }
		}
	}
});
