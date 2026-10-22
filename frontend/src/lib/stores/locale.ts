// Idioma ativo: preferência local do navegador (não existe no contrato, nunca vai ao servidor).
// Detecção: valor salvo válido → correspondência exata em `navigator.languages` → mesmo
// idioma base (`pt-PT` → `pt-BR`) → `DETECTION_FALLBACK`. Trocar idioma pela UI usa
// `setLocale` de `#lib/i18n/index.ts`, que carrega o catálogo antes de confirmar aqui.

import { browser } from '$app/env';
import { derived, writable, type Readable } from 'svelte/store';
import {
	availableLocales,
	DETECTION_FALLBACK,
	findLocale,
	REFERENCE_LOCALE,
	STORAGE_KEY,
	type LocaleInfo
} from '#lib/i18n/config.ts';

function readSaved(): string | null {
	try {
		return localStorage.getItem(STORAGE_KEY);
	} catch {
		return null;
	}
}

function baseLanguage(code: string): string {
	return code.split('-')[0]?.toLowerCase() ?? '';
}

/** Resolve o idioma inicial; fora do navegador devolve o idioma de referência. */
export function detectLocale(): string {
	if (!browser) return REFERENCE_LOCALE;
	const saved = readSaved();
	if (saved && findLocale(saved)) return saved;

	const preferred = navigator.languages?.length ? navigator.languages : [navigator.language];
	for (const lang of preferred) {
		const exact = availableLocales.find((l) => l.code.toLowerCase() === lang.toLowerCase());
		if (exact) return exact.code;
	}
	for (const lang of preferred) {
		const sameBase = availableLocales.find((l) => baseLanguage(l.code) === baseLanguage(lang));
		if (sameBase) return sameBase.code;
	}
	return findLocale(DETECTION_FALLBACK) ? DETECTION_FALLBACK : REFERENCE_LOCALE;
}

const locale = writable<string>(detectLocale());

/** Código BCP 47 do idioma ativo, sempre presente em `availableLocales`. */
export const localeStore: Readable<string> = { subscribe: locale.subscribe };

/** Metadados do idioma ativo (`name`, `dir`, `flagUrl`) para o seletor e `<html lang dir>`. */
export const localeMeta: Readable<LocaleInfo> = derived(
	locale,
	($locale): LocaleInfo => findLocale($locale) ?? { code: $locale, name: $locale, dir: 'ltr', flagUrl: null }
);

/**
 * Confirma o idioma ativo (uso interno de `setLocale`/`initI18n`, após carregar o catálogo).
 * Código inexistente é ignorado; `persist` grava a escolha explícita no `localStorage`.
 */
export function commitLocale(code: string, persist: boolean): boolean {
	if (!findLocale(code)) return false;
	locale.set(code);
	if (persist && browser) {
		try {
			localStorage.setItem(STORAGE_KEY, code);
		} catch {
			// Armazenamento indisponível (modo privado/cota): a escolha vale só nesta sessão.
		}
	}
	return true;
}
