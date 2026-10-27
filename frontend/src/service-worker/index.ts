// Service Worker da PWA (guidelines §3.4): carrega a interface mesmo offline; métricas
// exigem conexão. Cache First versionado para os bundles gerados, os arquivos de `static`
// e o shell prerenderizado; `/api/v1/`, `/ws/` e `/_app/version.json` vão sempre à rede
// (não são interceptados). No `vite dev` apenas repassa à rede e limpa caches anteriores.

import { dev, version } from '$app/env';
import { assets, immutable, prerendered } from '$app/manifest';
import { self } from '$app/service-worker';

const CACHE_PREFIX = 'phonix-';
const CACHE = `${CACHE_PREFIX}${version}`;

/** Base path = diretório do próprio SW (`new URL('', href)` devolveria o script, não `/`). */
const BASE_URL = new URL('./', self.location.href);

/** Converte um caminho de `$app/manifest` (relativo ao base path, sem `/` inicial) em `pathname`. */
function toPathname(path: string): string {
	return new URL(path, BASE_URL).pathname;
}

/** Shell da SPA: a rota raiz prerenderizada (`{ path: '' }`), equivalente ao `200.html` com `ssr = false`. */
const ROOT = toPathname('');
/** Consultado pelo SvelteKit (1 h, foco/visibilidade) para detectar novo deploy: sempre rede. */
const VERSION_FILE = toPathname('_app/version.json');
/** Tráfego dinâmico do backend: nunca interceptado nem armazenado. */
const NETWORK_ONLY_PREFIXES = [toPathname('api/v1/'), toPathname('ws/')];

/** `immutable` inclui CSS, fontes `.woff2` e os chunks de tradução de todos os idiomas. */
const PRECACHE = new Set(
	[...immutable, ...assets, ...prerendered]
		.map(({ path }) => toPathname(path))
		.filter((pathname) => !isNetworkOnly(pathname))
);

function isNetworkOnly(pathname: string): boolean {
	return (
		pathname === VERSION_FILE || NETWORK_ONLY_PREFIXES.some((prefix) => pathname.startsWith(prefix))
	);
}

async function precache(): Promise<void> {
	const cache = await caches.open(CACHE);
	await cache.addAll([...PRECACHE]);
}

/** Remove caches de versões anteriores (no dev, todos os caches do app). */
async function pruneCaches(): Promise<void> {
	for (const key of await caches.keys()) {
		if (key.startsWith(CACHE_PREFIX) && (dev || key !== CACHE)) {
			await caches.delete(key);
		}
	}
}

async function cacheFirst(request: Request, pathname: string): Promise<Response> {
	const cache = await caches.open(CACHE);
	const cached = await cache.match(pathname);
	if (cached) return cached;

	const response = await fetch(request);
	if (response.ok && response.type === 'basic') {
		await cache.put(pathname, response.clone());
	}
	return response;
}

/** Navegação sem arquivo correspondente: rede; offline, o shell da raiz já cacheado. */
async function navigate(request: Request): Promise<Response> {
	try {
		return await fetch(request);
	} catch (error) {
		const shell = PRECACHE.has(ROOT) ? await caches.match(ROOT, { cacheName: CACHE }) : undefined;
		if (shell) return shell;
		throw error;
	}
}

self.addEventListener('install', (event) => {
	if (dev) {
		self.skipWaiting();
		return;
	}
	event.waitUntil(precache());
});

self.addEventListener('activate', (event) => {
	event.waitUntil(pruneCaches());
});

self.addEventListener('fetch', (event) => {
	const { request } = event;
	if (dev || request.method !== 'GET') return;

	const url = new URL(request.url);
	// Outras origens (ex.: `icon_url` externos dos atalhos) e tráfego dinâmico seguem direto à rede.
	if (url.origin !== self.location.origin || isNetworkOnly(url.pathname)) return;

	if (PRECACHE.has(url.pathname)) {
		event.respondWith(cacheFirst(request, url.pathname));
	} else if (request.mode === 'navigate') {
		event.respondWith(navigate(request));
	}
});
