#!/usr/bin/env node
// Verificação do i18n do PhonixOS (Node puro, sem dependências). Executado por `npm run check`.
//   1. Catálogos: meta.json, mesmos namespaces/chaves/plurais/placeholders do idioma de
//      referência e nenhuma string vazia.
//   2. Registro: `<ns>.json` do idioma de referência ⇔ `namespaces.ts`; nomes em camelCase.
//   3. Texto literal no markup `.svelte` (nós de texto e aria-label/title/placeholder/alt).
//   4. Strings literais suspeitas em expressões `{…}`, `<script>` e `src/**/*.ts`.
//   5. Chaves do idioma de referência sem uso em `src/` (aviso, não reprova).
// Exceções: `<!-- i18n-ignore: motivo -->` ou `// i18n-ignore: motivo` na linha anterior.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const SRC = join(ROOT, 'src');
const I18N = join(SRC, 'lib', 'i18n');
const LOCALES = join(I18N, 'locales');

const PLURAL_SUFFIXES = ['zero', 'one', 'two', 'few', 'many', 'other'];
const KEY_RE = new RegExp(`^[a-z][a-zA-Z0-9]*(?:_(?:${PLURAL_SUFFIXES.join('|')}))?$`);
const NAMESPACE_RE = /^[a-z][a-zA-Z0-9]*$/;
const PROPER_NAMES = /PhonixOS/g;
const TEXT_ATTRIBUTES = new Set(['aria-label', 'title', 'placeholder', 'alt']);
const STYLE_ATTRIBUTE_RE = /^(?:class|style)(?::|$)/;
const STYLE_IDENTIFIER_RE = /class|style/i;

const errors = [];
const warnings = [];
const ignores = [];

const rel = (file) => relative(ROOT, file).split(sep).join('/');
const report = (list, file, line, message) => list.push({ file: rel(file), line, message });

function walk(dir, filter, out = []) {
	if (!existsSync(dir)) return out;
	for (const entry of readdirSync(dir)) {
		const full = join(dir, entry);
		if (statSync(full).isDirectory()) walk(full, filter, out);
		else if (filter(full)) out.push(full);
	}
	return out.sort();
}

function lineIndex(text) {
	const starts = [0];
	for (let i = 0; i < text.length; i++) if (text[i] === '\n') starts.push(i + 1);
	return (offset) => {
		let lo = 0;
		let hi = starts.length - 1;
		while (lo < hi) {
			const mid = (lo + hi + 1) >> 1;
			if (starts[mid] <= offset) lo = mid;
			else hi = mid - 1;
		}
		return lo + 1;
	};
}

const hasLetters = (text) => /\p{L}/u.test(text);

/** Suspeita = contém letras e (espaço, inicial maiúscula ou letra não ASCII). */
function isSuspicious(value) {
	const text = value.replace(PROPER_NAMES, '');
	if (!hasLetters(text)) return false;
	return /\s/.test(text) || /^\s*\p{Lu}/u.test(text) || /[^\x00-\x7F]/.test(text.replace(/[^\p{L}]/gu, ''));
}

// ───────────────────────────── 1. Catálogos ─────────────────────────────

function readReferenceLocale() {
	const config = readFileSync(join(I18N, 'config.ts'), 'utf8');
	const match = config.match(/REFERENCE_LOCALE\s*=\s*['"]([^'"]+)['"]/);
	if (!match) {
		report(errors, join(I18N, 'config.ts'), 1, 'REFERENCE_LOCALE não encontrado');
		return null;
	}
	return match[1];
}

function keyLine(text, key) {
	const at = text.indexOf(`"${key}"`);
	return at < 0 ? 1 : lineIndex(text)(at);
}

function readJson(file) {
	const text = readFileSync(file, 'utf8');
	try {
		return { text, data: JSON.parse(text) };
	} catch (error) {
		report(errors, file, 1, `JSON inválido: ${error.message}`);
		return { text, data: null };
	}
}

const placeholders = (message) => new Set([...message.matchAll(/\{(\w+)\}/g)].map((m) => m[1]));
const sameSet = (a, b) => a.size === b.size && [...a].every((x) => b.has(x));
const fmtSet = (s) => (s.size ? [...s].map((x) => `{${x}}`).join(', ') : 'nenhum');

/** Agrupa as chaves de um catálogo por chave-base: { plural: Set<categoria>|null, placeholders }. */
function loadCatalog(file) {
	const { text, data } = readJson(file);
	const groups = new Map();
	if (data === null) return { text, groups, ok: false };
	if (typeof data !== 'object' || Array.isArray(data)) {
		report(errors, file, 1, 'o catálogo deve ser um objeto JSON plano');
		return { text, groups, ok: false };
	}
	for (const [key, value] of Object.entries(data)) {
		const line = keyLine(text, key);
		if (!KEY_RE.test(key)) report(errors, file, line, `chave "${key}" fora de camelCase (sufixos de plural: _${PLURAL_SUFFIXES.join(', _')})`);
		if (typeof value !== 'string') {
			report(errors, file, line, `"${key}" deve ser string (JSON plano, sem aninhamento)`);
			continue;
		}
		if (value.trim() === '') report(errors, file, line, `"${key}" está vazia`);
		const match = key.match(new RegExp(`^(.*)_(${PLURAL_SUFFIXES.join('|')})$`));
		const base = match ? match[1] : key;
		const group = groups.get(base) ?? { plural: null, plain: false, placeholders: new Set(), line };
		if (match) (group.plural ??= new Set()).add(match[2]);
		else group.plain = true;
		for (const p of placeholders(value)) group.placeholders.add(p);
		groups.set(base, group);
	}
	for (const [base, group] of groups) {
		if (group.plural && group.plain) report(errors, file, group.line, `"${base}" mistura forma simples e plural`);
		if (group.plural && !group.plural.has('other')) report(errors, file, group.line, `"${base}" no plural exige "${base}_other"`);
	}
	return { text, groups, ok: true };
}

function namespacesOf(localeDir) {
	return readdirSync(localeDir)
		.filter((f) => f.endsWith('.json') && f !== 'meta.json')
		.map((f) => f.slice(0, -'.json'.length))
		.sort();
}

function checkCatalogs(reference) {
	if (!existsSync(LOCALES)) {
		report(errors, LOCALES, 1, 'diretório de idiomas ausente');
		return null;
	}
	const locales = readdirSync(LOCALES).filter((d) => statSync(join(LOCALES, d)).isDirectory()).sort();
	if (!locales.includes(reference)) {
		report(errors, join(LOCALES, reference), 1, `idioma de referência "${reference}" ausente`);
		return null;
	}

	const referenceNamespaces = namespacesOf(join(LOCALES, reference));
	const referenceCatalogs = new Map(
		referenceNamespaces.map((ns) => [ns, loadCatalog(join(LOCALES, reference, `${ns}.json`))])
	);

	for (const locale of locales) {
		const dir = join(LOCALES, locale);
		const metaFile = join(dir, 'meta.json');
		if (!existsSync(metaFile)) {
			report(errors, metaFile, 1, 'meta.json ausente');
		} else {
			const { data, text } = readJson(metaFile);
			if (data !== null) {
				if (typeof data.name !== 'string' || data.name.trim() === '') report(errors, metaFile, keyLine(text, 'name'), '"name" deve ser o nome nativo do idioma');
				if (data.dir !== 'ltr' && data.dir !== 'rtl') report(errors, metaFile, keyLine(text, 'dir'), '"dir" deve ser "ltr" ou "rtl"');
			}
		}

		const namespaces = namespacesOf(dir);
		for (const ns of namespaces) {
			if (!NAMESPACE_RE.test(ns)) report(errors, join(dir, `${ns}.json`), 1, `namespace "${ns}" fora de camelCase`);
		}
		if (locale === reference) continue;

		for (const ns of referenceNamespaces) {
			if (!namespaces.includes(ns)) report(errors, join(dir, `${ns}.json`), 1, `namespace "${ns}" ausente (existe em ${reference})`);
		}
		for (const ns of namespaces) {
			const file = join(dir, `${ns}.json`);
			const ref = referenceCatalogs.get(ns);
			if (!ref) {
				report(errors, file, 1, `namespace "${ns}" não existe no idioma de referência ${reference}`);
				continue;
			}
			const catalog = loadCatalog(file);
			if (!catalog.ok || !ref.ok) continue;
			for (const [base, refGroup] of ref.groups) {
				const group = catalog.groups.get(base);
				if (!group) {
					report(errors, file, 1, `chave "${ns}.${base}" ausente (existe em ${reference})`);
					continue;
				}
				if (Boolean(refGroup.plural) !== Boolean(group.plural)) {
					report(errors, file, group.line, `"${ns}.${base}" deve ser ${refGroup.plural ? 'plural (_other…)' : 'simples'} como em ${reference}`);
				}
				if (!sameSet(refGroup.placeholders, group.placeholders)) {
					report(errors, file, group.line, `"${ns}.${base}" usa ${fmtSet(group.placeholders)}; ${reference} usa ${fmtSet(refGroup.placeholders)}`);
				}
			}
			for (const [base, group] of catalog.groups) {
				if (!ref.groups.has(base)) report(errors, file, group.line, `chave "${ns}.${base}" não existe em ${reference}`);
			}
		}
	}
	return referenceCatalogs;
}

// ──────────────────────── 2. Registro de namespaces ────────────────────────

function checkRegistry(reference, referenceCatalogs) {
	const file = join(I18N, 'namespaces.ts');
	if (!existsSync(file)) {
		report(errors, file, 1, 'namespaces.ts ausente');
		return;
	}
	const text = readFileSync(file, 'utf8');
	const lineOf = lineIndex(text);
	const imports = new Map();
	for (const m of text.matchAll(/import\s+type\s+(\w+)\s+from\s+['"]\.\/locales\/([^/'"]+)\/([^/'"]+)\.json['"]/g)) {
		const line = lineOf(m.index);
		if (m[2] !== reference) report(errors, file, line, `"${m[3]}" deve ser importado de ./locales/${reference}/`);
		imports.set(m[1], { ns: m[3], line });
	}
	const body = text.match(/interface\s+Namespaces\s*\{([^}]*)\}/);
	const registered = new Map();
	if (!body) {
		report(errors, file, 1, 'interface Namespaces não encontrada');
	} else {
		const bodyOffset = body.index + body[0].indexOf('{') + 1;
		for (const m of body[1].matchAll(/(\w+)\s*:\s*typeof\s+(\w+)/g)) {
			const line = lineOf(bodyOffset + m.index);
			const source = imports.get(m[2]);
			if (!source) report(errors, file, line, `"${m[1]}" referencia "${m[2]}", que não é um import type de catálogo`);
			else if (source.ns !== m[1]) report(errors, file, line, `"${m[1]}" deve apontar para ${m[1]}.json (aponta para ${source.ns}.json)`);
			registered.set(m[1], line);
		}
	}
	for (const [ns, line] of registered) {
		if (!NAMESPACE_RE.test(ns)) report(errors, file, line, `namespace "${ns}" fora de camelCase`);
		if (referenceCatalogs && !referenceCatalogs.has(ns)) report(errors, file, line, `namespace "${ns}" registrado sem locales/${reference}/${ns}.json`);
	}
	for (const ns of referenceCatalogs?.keys() ?? []) {
		if (!registered.has(ns)) report(errors, join(LOCALES, reference, `${ns}.json`), 1, `namespace "${ns}" sem registro em src/lib/i18n/namespaces.ts`);
	}
}

// ─────────────────────── Tokenizador JS/TS simplificado ───────────────────────

const REGEX_KEYWORDS = new Set(['return', 'typeof', 'instanceof', 'in', 'of', 'new', 'delete', 'void', 'throw', 'case', 'do', 'else', 'yield', 'await']);
const PUNCTUATORS = ['>>>=', '...', '===', '!==', '**=', '<<=', '>>=', '>>>', '&&=', '||=', '??=', '=>', '==', '!=', '<=', '>=', '&&', '||', '??', '?.', '++', '--', '+=', '-=', '*=', '/=', '%=', '&=', '|=', '^=', '**', '<<', '>>'];

/**
 * Tokeniza código a partir de `start`. Com `untilBrace`, para no `}` que fecha a expressão
 * de markup. Devolve tokens (strings e partes estáticas de template como `str`), comentários
 * `// i18n-ignore` e o offset final.
 */
function tokenize(text, start, untilBrace) {
	const tokens = [];
	const comments = [];
	const braces = [];
	let i = start;
	const prevSignificant = () => tokens[tokens.length - 1];

	const readQuoted = (quote) => {
		const begin = i;
		let value = '';
		i++;
		while (i < text.length && text[i] !== quote && text[i] !== '\n') {
			if (text[i] === '\\') {
				value += text[i + 1] ?? '';
				i += 2;
			} else value += text[i++];
		}
		i++;
		tokens.push({ type: 'str', value, start: begin });
	};

	const readTemplatePart = () => {
		// `i` aponta para o primeiro caractere depois de ` ou de }.
		const begin = i;
		let value = '';
		while (i < text.length) {
			if (text[i] === '\\') {
				value += text[i + 1] ?? '';
				i += 2;
			} else if (text[i] === '`') {
				i++;
				break;
			} else if (text[i] === '$' && text[i + 1] === '{') {
				i += 2;
				braces.push('template');
				break;
			} else value += text[i++];
		}
		tokens.push({ type: 'str', value, start: begin, template: true });
	};

	while (i < text.length) {
		const c = text[i];
		if (/\s/.test(c)) {
			i++;
			continue;
		}
		if (c === '/' && text[i + 1] === '/') {
			const end = text.indexOf('\n', i);
			const body = text.slice(i + 2, end < 0 ? text.length : end);
			const ignore = body.match(/^\s*i18n-ignore\s*:?\s*(.*)$/);
			if (ignore) comments.push({ offset: i, reason: ignore[1].trim() });
			i = end < 0 ? text.length : end;
			continue;
		}
		if (c === '/' && text[i + 1] === '*') {
			const end = text.indexOf('*/', i + 2);
			i = end < 0 ? text.length : end + 2;
			continue;
		}
		if (c === '"' || c === "'") {
			readQuoted(c);
			continue;
		}
		if (c === '`') {
			i++;
			readTemplatePart();
			continue;
		}
		if (c === '{') {
			braces.push('block');
			tokens.push({ type: 'punct', value: c, start: i++ });
			continue;
		}
		if (c === '}') {
			const top = braces.pop();
			if (top === 'template') {
				i++;
				readTemplatePart();
				continue;
			}
			if (top === undefined && untilBrace) return { tokens, comments, end: i };
			tokens.push({ type: 'punct', value: c, start: i++ });
			continue;
		}
		if (/[\p{L}_$]/u.test(c)) {
			const m = text.slice(i).match(/^[\p{L}\p{N}_$]+/u);
			tokens.push({ type: 'ident', value: m[0], start: i });
			i += m[0].length;
			continue;
		}
		if (/[0-9]/.test(c) || (c === '.' && /[0-9]/.test(text[i + 1] ?? ''))) {
			const m = text.slice(i).match(/^[0-9a-fA-FxXoObBn_.eE]+/);
			tokens.push({ type: 'num', value: m[0], start: i });
			i += m[0].length;
			continue;
		}
		if (c === '/') {
			const prev = prevSignificant();
			const regexAllowed =
				!prev ||
				(prev.type === 'punct' && ![')', ']', '}'].includes(prev.value)) ||
				(prev.type === 'ident' && REGEX_KEYWORDS.has(prev.value));
			if (regexAllowed) {
				const begin = i++;
				let inClass = false;
				while (i < text.length && text[i] !== '\n') {
					if (text[i] === '\\') i += 2;
					else if (text[i] === '[') (inClass = true), i++;
					else if (text[i] === ']') (inClass = false), i++;
					else if (text[i] === '/' && !inClass) {
						i++;
						break;
					} else i++;
				}
				while (/[a-z]/i.test(text[i] ?? '')) i++;
				tokens.push({ type: 'regex', value: text.slice(begin, i), start: begin });
				continue;
			}
		}
		const punct = PUNCTUATORS.find((p) => text.startsWith(p, i)) ?? c;
		tokens.push({ type: 'punct', value: punct, start: i });
		i += punct.length;
	}
	return { tokens, comments, end: i };
}

/** Strings suspeitas que não se enquadram nas exceções de contexto. */
function suspiciousStrings(tokens) {
	const found = [];
	const consoleDepths = [];
	const typeScopes = [];
	let depth = 0;
	let typeAliasDepth = null;
	let pendingInterface = false;

	for (let k = 0; k < tokens.length; k++) {
		const tok = tokens[k];
		const prev = tokens[k - 1];
		const prev2 = tokens[k - 2];
		const next = tokens[k + 1];

		if (tok.type === 'punct') {
			if (tok.value === '(' || tok.value === '[' || tok.value === '{') {
				depth++;
				if (tok.value === '{' && pendingInterface) {
					typeScopes.push(depth);
					pendingInterface = false;
				}
			} else if (tok.value === ')' || tok.value === ']' || tok.value === '}') {
				if (consoleDepths.at(-1) === depth) consoleDepths.pop();
				if (typeScopes.at(-1) === depth) typeScopes.pop();
				depth--;
			} else if (tok.value === ';' && typeAliasDepth === depth) typeAliasDepth = null;
			if (tok.value === '(' && prev?.type === 'ident' && prev2?.value === '.' && tokens[k - 3]?.value === 'console') {
				consoleDepths.push(depth);
			}
			continue;
		}
		if (tok.type === 'ident') {
			if (tok.value === 'interface' && next?.type === 'ident') pendingInterface = true;
			if (tok.value === 'type' && next?.type === 'ident' && (prev === undefined || ['ident', 'punct'].includes(prev.type)) && typeAliasDepth === null) {
				const after = tokens.slice(k + 2, k + 40).find((t) => t.value === '=' || t.value === ';');
				if (after?.value === '=' && (prev === undefined || prev.value === 'export' || prev.value === ';' || prev.value === '}' || prev.value === '{')) {
					typeAliasDepth = depth;
				}
			}
			continue;
		}
		if (tok.type !== 'str' || !isSuspicious(tok.value)) continue;

		const exempt =
			consoleDepths.length > 0 ||
			typeScopes.length > 0 ||
			typeAliasDepth !== null ||
			// import/export … from '…', import '…', import('…')
			prev?.value === 'from' ||
			(prev?.value === 'import' && !tok.template) ||
			(prev?.value === '(' && prev2?.value === 'import') ||
			// chave passada a t/$t
			(prev?.value === '(' && (prev2?.value === 't' || prev2?.value === '$t')) ||
			// comparações e case
			['===', '!==', '==', '!='].includes(prev?.value) ||
			['===', '!==', '==', '!='].includes(next?.value) ||
			prev?.value === 'case' ||
			// uniões/interseções e asserções de tipo
			prev?.value === '|' ||
			prev?.value === '&' ||
			next?.value === '|' ||
			next?.value === '&' ||
			prev?.value === 'as' ||
			prev?.value === 'satisfies' ||
			// chave de objeto { 'Not charging': … }
			(next?.value === ':' && (prev?.value === '{' || prev?.value === ',')) ||
			// valores de classe/estilo atribuídos a identificadores (cardClass = '…', style: '…')
			((prev?.value === '=' || prev?.value === ':') && prev2?.type === 'ident' && STYLE_IDENTIFIER_RE.test(prev2.value));

		if (!exempt) found.push(tok);
	}
	return found;
}

// ─────────────────────────── 3/4. Varredura de código ───────────────────────────

function makeIgnores(file, lineOf) {
	const lines = new Map();
	return {
		add(offset, reason) {
			const line = lineOf(offset);
			if (!reason) report(errors, file, line, 'i18n-ignore exige um motivo (i18n-ignore: motivo)');
			lines.set(line + 1, reason);
			ignores.push({ file: rel(file), line, message: reason || '(sem motivo)' });
		},
		has: (line) => lines.has(line)
	};
}

function scanCode(file, text, start, untilBrace, lineOf, ignore, exemptAll) {
	const { tokens, comments, end } = tokenize(text, start, untilBrace);
	for (const comment of comments) ignore.add(comment.offset, comment.reason);
	if (!exemptAll) {
		for (const tok of suspiciousStrings(tokens)) {
			const line = lineOf(tok.start);
			if (!ignore.has(line)) report(errors, file, line, `texto literal ${JSON.stringify(tok.value)} em código — use t('<namespace>.<chave>')`);
		}
	}
	return end;
}

function scanTs(file) {
	const text = readFileSync(file, 'utf8');
	const lineOf = lineIndex(text);
	scanCode(file, text, 0, false, lineOf, makeIgnores(file, lineOf), false);
}

function reportText(file, value, offset, lineOf, ignore, where) {
	const stripped = value.replace(PROPER_NAMES, '').replace(/&#?\w+;/g, '');
	if (!hasLetters(stripped)) return;
	const trimmed = value.trim();
	const lead = value.length - value.trimStart().length;
	const line = lineOf(offset + lead);
	if (!ignore.has(line)) report(errors, file, line, `texto literal ${JSON.stringify(trimmed)} ${where} — use {$t('<namespace>.<chave>')}`);
}

function scanSvelte(file) {
	const raw = readFileSync(file, 'utf8');
	const lineOf = lineIndex(raw);
	const ignore = makeIgnores(file, lineOf);

	// <script> é varrido como código; <script> e <style> são removidos do markup preservando offsets.
	let markup = raw;
	const blank = (from, to) => (markup = markup.slice(0, from) + markup.slice(from, to).replace(/[^\n]/g, ' ') + markup.slice(to));
	for (const m of raw.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\s*>/g)) {
		const contentStart = m.index + m[0].indexOf('>') + 1;
		scanCode(file, raw.slice(0, contentStart + m[1].length), contentStart, false, lineOf, ignore, false);
		blank(m.index, m.index + m[0].length);
	}
	for (const m of raw.matchAll(/<style\b[^>]*>[\s\S]*?<\/style\s*>/g)) blank(m.index, m.index + m[0].length);

	const expression = (at, attribute) => {
		const exemptAll = attribute !== null && STYLE_ATTRIBUTE_RE.test(attribute);
		// Tags de bloco: `{/if}` não tem expressão; `{#if …}`, `{:else if …}`, `{@render …}` pulam a palavra-chave.
		const tag = markup.slice(at + 1).match(/^\s*(?:\/[^}]*|[#:@][\w-]*)/);
		if (tag && tag[0].trimStart().startsWith('/')) return at + 1 + tag[0].length + 1;
		const start = at + 1 + (tag ? tag[0].length : 0);
		return scanCode(file, markup, start, true, lineOf, ignore, exemptAll) + 1;
	};

	let i = 0;
	let textStart = 0;
	let textValue = '';
	const flushText = () => {
		if (textValue) reportText(file, textValue, textStart, lineOf, ignore, 'no markup');
		textValue = '';
	};

	while (i < markup.length) {
		if (markup.startsWith('<!--', i)) {
			flushText();
			const end = markup.indexOf('-->', i);
			const body = markup.slice(i + 4, end < 0 ? markup.length : end);
			const m = body.match(/^\s*i18n-ignore\s*:?\s*([\s\S]*?)\s*$/);
			if (m) ignore.add(i, m[1]);
			i = end < 0 ? markup.length : end + 3;
			continue;
		}
		if (markup[i] === '{') {
			flushText();
			i = expression(i, null);
			continue;
		}
		if (markup[i] === '<' && /[A-Za-z/!]/.test(markup[i + 1] ?? '')) {
			flushText();
			if (markup[i + 1] === '/' || markup[i + 1] === '!') {
				i = markup.indexOf('>', i) + 1 || markup.length;
				continue;
			}
			i++;
			while (i < markup.length && /[^\s/>]/.test(markup[i])) i++;
			// Atributos até `>` ou `/>`.
			while (i < markup.length) {
				while (/\s/.test(markup[i] ?? '')) i++;
				if (markup[i] === '>') {
					i++;
					break;
				}
				if (markup[i] === '/' && markup[i + 1] === '>') {
					i += 2;
					break;
				}
				if (markup[i] === '{') {
					i = expression(i, null);
					continue;
				}
				const nameStart = i;
				while (i < markup.length && /[^\s=/>]/.test(markup[i])) i++;
				const name = markup.slice(nameStart, i);
				if (!name) {
					i++;
					continue;
				}
				while (/\s/.test(markup[i] ?? '')) i++;
				if (markup[i] !== '=') continue;
				i++;
				while (/\s/.test(markup[i] ?? '')) i++;
				const quote = markup[i];
				if (quote === '{') {
					i = expression(i, name);
				} else if (quote === '"' || quote === "'") {
					i++;
					let partStart = i;
					let part = '';
					const flushPart = () => {
						if (TEXT_ATTRIBUTES.has(name) && part) reportText(file, part, partStart, lineOf, ignore, `em ${name}`);
						part = '';
					};
					while (i < markup.length && markup[i] !== quote) {
						if (markup[i] === '{') {
							flushPart();
							i = expression(i, name);
							partStart = i;
						} else part += markup[i++];
					}
					flushPart();
					i++;
				} else {
					const valueStart = i;
					while (i < markup.length && /[^\s>]/.test(markup[i])) i++;
					if (TEXT_ATTRIBUTES.has(name)) reportText(file, markup.slice(valueStart, i), valueStart, lineOf, ignore, `em ${name}`);
				}
			}
			continue;
		}
		if (!textValue) textStart = i;
		textValue += markup[i++];
	}
	flushText();
}

// ───────────────────────────── 5. Chaves sem uso ─────────────────────────────

function checkUnused(reference, referenceCatalogs, sources) {
	const corpus = sources.map((file) => readFileSync(file, 'utf8')).join('\n');
	for (const [ns, catalog] of referenceCatalogs ?? []) {
		const file = join(LOCALES, reference, `${ns}.json`);
		for (const [base, group] of catalog.groups) {
			const key = `${ns}.${base}`;
			if (!['"', "'", '`'].some((q) => corpus.includes(`${q}${key}${q}`))) {
				report(warnings, file, group.line, `chave "${key}" sem uso em src/`);
			}
		}
	}
}

// ─────────────────────────────────── main ───────────────────────────────────

const reference = readReferenceLocale();
const referenceCatalogs = reference ? checkCatalogs(reference) : null;
if (reference) checkRegistry(reference, referenceCatalogs);

const insideI18n = (file) => file.startsWith(I18N + sep);
const svelteFiles = walk(SRC, (f) => f.endsWith('.svelte'));
const tsFiles = walk(SRC, (f) => f.endsWith('.ts') && !f.endsWith('.d.ts') && !insideI18n(f));
for (const file of svelteFiles) scanSvelte(file);
for (const file of tsFiles) scanTs(file);
if (reference) checkUnused(reference, referenceCatalogs, walk(SRC, (f) => /\.(svelte|ts|js)$/.test(f) && !insideI18n(f)));

const print = (list) => list.forEach((e) => console.log(`  ${e.file}:${e.line}  ${e.message}`));
if (ignores.length) {
	console.log(`i18n-ignore (${ignores.length}):`);
	print(ignores);
}
if (warnings.length) {
	console.log(`Avisos (${warnings.length}):`);
	print(warnings);
}
if (errors.length) {
	console.log(`Erros de i18n (${errors.length}):`);
	print(errors);
	process.exit(1);
}
console.log(`check-i18n: OK (${svelteFiles.length} .svelte, ${tsFiles.length} .ts, ${referenceCatalogs?.size ?? 0} namespaces)`);
