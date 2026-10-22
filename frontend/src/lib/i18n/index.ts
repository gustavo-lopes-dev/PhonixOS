// Runtime do i18n: catálogos JSON por namespace carregados sob demanda (um chunk por
// arquivo), `t` tipado como store derivada e troca de idioma sem piscar texto.
// Fallback de chave: idioma ativo → referência → a própria chave (aviso só em dev).

import { dev } from '$app/env';
import { derived, get, writable, type Readable } from 'svelte/store';
import { commitLocale, localeStore } from '#lib/stores/locale.ts';
import { findLocale, REFERENCE_LOCALE } from './config.ts';
import type { MessageKey } from './namespaces.ts';

export { availableLocales, type LocaleInfo } from './config.ts';
export type { MessageKey } from './namespaces.ts';

type Catalog = Record<string, string>;
/** Catálogos carregados: idioma → namespace → mensagens. */
type LoadedCatalogs = Record<string, Record<string, Catalog>>;

export type TranslateParams = Record<string, string | number>;
export type Translate = (key: MessageKey, params?: TranslateParams) => string;

const catalogFiles = import.meta.glob<Catalog>(['./locales/*/*.json', '!./locales/*/meta.json'], {
	import: 'default'
});

const loaded = writable<LoadedCatalogs>({});
const pending = new Map<string, Promise<void>>();

function loadLocale(code: string): Promise<void> {
	const cached = pending.get(code);
	if (cached) return cached;
	const prefix = `./locales/${code}/`;
	const files = Object.entries(catalogFiles).filter(([path]) => path.startsWith(prefix));
	const promise = Promise.all(
		files.map(async ([path, load]) => [path.slice(prefix.length, -'.json'.length), await load()] as const)
	).then((entries) => {
		loaded.update((all) => ({ ...all, [code]: Object.fromEntries(entries) }));
	});
	pending.set(code, promise);
	promise.catch(() => pending.delete(code));
	return promise;
}

/**
 * Carrega o idioma de referência e o detectado; deve ser aguardado antes do primeiro
 * render (3.6, `+layout.svelte`). Se o catálogo ativo falhar, recai na referência.
 */
export async function initI18n(): Promise<void> {
	const active = get(localeStore);
	await loadLocale(REFERENCE_LOCALE);
	try {
		await loadLocale(active);
	} catch (error) {
		console.warn('[i18n] falha ao carregar o idioma', active, error);
		commitLocale(REFERENCE_LOCALE, false);
	}
}

/** Troca o idioma: carrega o catálogo antes de ativar e grava a preferência local. */
export async function setLocale(code: string): Promise<boolean> {
	if (!findLocale(code)) return false;
	await loadLocale(code);
	return commitLocale(code, true);
}

const pluralRules = new Map<string, Intl.PluralRules>();
const numberFormats = new Map<string, Intl.NumberFormat>();

function cached<T>(cache: Map<string, T>, locale: string, create: () => T): T {
	let value = cache.get(locale);
	if (value === undefined) {
		value = create();
		cache.set(locale, value);
	}
	return value;
}

function pick(catalog: Catalog, name: string, locale: string, count: unknown): string | undefined {
	if (typeof count === 'number') {
		const category = cached(pluralRules, locale, () => new Intl.PluralRules(locale)).select(count);
		return catalog[`${name}_${category}`] ?? catalog[`${name}_other`] ?? catalog[name];
	}
	return catalog[name] ?? catalog[`${name}_other`];
}

function interpolate(message: string, locale: string, params: TranslateParams | undefined): string {
	if (!params) return message;
	return message.replace(/\{(\w+)\}/g, (match, name: string) => {
		const value = params[name];
		if (value === undefined) return match;
		if (typeof value === 'number') {
			return cached(numberFormats, locale, () => new Intl.NumberFormat(locale)).format(value);
		}
		return value;
	});
}

const warned = new Set<string>();

function translate(locale: string, catalogs: LoadedCatalogs, key: MessageKey, params?: TranslateParams): string {
	const dot = key.indexOf('.');
	const namespace = key.slice(0, dot);
	const name = key.slice(dot + 1);
	const chain = locale === REFERENCE_LOCALE ? [locale] : [locale, REFERENCE_LOCALE];
	for (const candidate of chain) {
		const catalog = catalogs[candidate]?.[namespace];
		const message = catalog && pick(catalog, name, candidate, params?.count);
		if (message !== undefined) return interpolate(message, locale, params);
	}
	if (dev && !warned.has(key)) {
		warned.add(key);
		console.warn('[i18n] chave sem tradução:', key);
	}
	return key;
}

/** `$t('connection.reconnectNow')`, `$t('launcher.shortcutCount', { count: 8 })`. */
export const t: Readable<Translate> = derived(
	[localeStore, loaded],
	([$locale, $loaded]) =>
		(key: MessageKey, params?: TranslateParams) =>
			translate($locale, $loaded, key, params)
);
