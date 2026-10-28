// Registro tipado dos namespaces: um `import type` + uma entrada por namespace, sempre a
// partir do idioma de referência. `scripts/check-i18n.js` exige paridade com os JSON.
// Posse: 3.2 → common, connection · 3.3 → metrics, layout, launcher · 3.3b → shell ·
// 3.5 → settings · 3.7 → dashboard.

import type common from './locales/pt-BR/common.json';
import type connection from './locales/pt-BR/connection.json';
import type dashboard from './locales/pt-BR/dashboard.json';
import type launcher from './locales/pt-BR/launcher.json';
import type layout from './locales/pt-BR/layout.json';
import type metrics from './locales/pt-BR/metrics.json';
import type settings from './locales/pt-BR/settings.json';
import type shell from './locales/pt-BR/shell.json';

export interface Namespaces {
	common: typeof common;
	connection: typeof connection;
	dashboard: typeof dashboard;
	launcher: typeof launcher;
	layout: typeof layout;
	metrics: typeof metrics;
	settings: typeof settings;
	shell: typeof shell;
}

type PluralSuffix = 'zero' | 'one' | 'two' | 'few' | 'many' | 'other';
type BaseKey<K extends string> = K extends `${infer Base}_${PluralSuffix}` ? Base : K;

/** União `` `${namespace}.${chave}` `` do idioma de referência (sem sufixo de plural). */
export type MessageKey = {
	[N in keyof Namespaces & string]: `${N}.${BaseKey<keyof Namespaces[N] & string>}`;
}[keyof Namespaces & string];
