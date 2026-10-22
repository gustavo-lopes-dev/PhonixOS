// Configuração do i18n e descoberta dos idiomas em build.
// Um idioma = uma pasta `locales/<código BCP 47>/` com `meta.json` (+ `flag.svg` opcional);
// nenhum código de idioma é fixado em tipo — só as constantes abaixo.

/** Idioma de referência: fonte das chaves tipadas e fallback de tradução. */
export const REFERENCE_LOCALE = 'pt-BR';
/** Usado quando nenhuma preferência do navegador corresponde a um idioma disponível. */
export const DETECTION_FALLBACK = 'en-US';
/** Chave do `localStorage` com a preferência local de idioma (nunca enviada ao servidor). */
export const STORAGE_KEY = 'phonix.locale';

export type TextDirection = 'ltr' | 'rtl';

interface LocaleMetaFile {
	name: string;
	dir: TextDirection;
}

export interface LocaleInfo {
	/** Código BCP 47 = nome da pasta (`pt-BR`). */
	code: string;
	/** Nome nativo (`Português`). */
	name: string;
	dir: TextDirection;
	/** URL da bandeira 24×16; `null` → a UI mostra um chip com o código. */
	flagUrl: string | null;
}

const metaFiles = import.meta.glob<LocaleMetaFile>('./locales/*/meta.json', {
	eager: true,
	import: 'default'
});
const flagFiles = import.meta.glob<string>('./locales/*/flag.svg', {
	eager: true,
	query: '?url',
	import: 'default'
});

function localeCode(path: string): string {
	return path.split('/')[2] ?? '';
}

/** Idiomas presentes em `locales/`, ordenados pelo código. */
export const availableLocales: readonly LocaleInfo[] = Object.entries(metaFiles)
	.map(([path, meta]) => {
		const code = localeCode(path);
		return { code, name: meta.name, dir: meta.dir, flagUrl: flagFiles[`./locales/${code}/flag.svg`] ?? null };
	})
	.sort((a, b) => a.code.localeCompare(b.code));

export function findLocale(code: string): LocaleInfo | undefined {
	return availableLocales.find((locale) => locale.code === code);
}
