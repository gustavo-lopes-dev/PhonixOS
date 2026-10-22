# i18n do PhonixOS

Catálogos JSON externos, um arquivo por namespace e uma pasta por idioma, sem dependências.
`pt-BR` é o idioma de referência: define as chaves tipadas e serve de fallback.

```text
src/lib/i18n/
├── index.ts        # initI18n(), setLocale(), availableLocales, t
├── config.ts       # REFERENCE_LOCALE, DETECTION_FALLBACK, STORAGE_KEY + descoberta dos idiomas
├── namespaces.ts   # registro tipado (import type do idioma de referência) e MessageKey
├── format.ts       # createFormatters(locale) e store `formatters` (Intl)
└── locales/<código BCP 47>/{meta.json, flag.svg?, <namespace>.json…}
```

## Adicionar um idioma

1. Copie `locales/pt-BR/` para `locales/<código>/` (ex.: `es-ES`).
2. Ajuste `meta.json` (`{ "name": "Español", "dir": "ltr" }` — nome nativo; `dir` é `ltr` ou `rtl`).
3. Troque `flag.svg` (24×16) ou apague-o: sem bandeira, a UI mostra um chip com o código.
4. Traduza todos os JSON, mantendo chaves e placeholders.
5. Rode `npm run check`.

Nenhum TypeScript muda: os idiomas são descobertos em build por `import.meta.glob`.

## Adicionar um namespace

1. Crie `<ns>.json` em **todas** as pastas de idioma (nome em camelCase, uma palavra de preferência,
   sem `.`, `-` ou `_`).
2. Registre em `namespaces.ts`: `import type <ns> from './locales/pt-BR/<ns>.json';` e
   `<ns>: typeof <ns>;` na interface `Namespaces`.
3. Rode `npm run check`.

Namespace = área funcional da interface, não componente nem etapa. Cada etapa é dona dos seus:
3.2 → `common`, `connection` · 3.3 → `metrics`, `layout`, `launcher` · 3.3b → `shell` ·
3.5 → `settings` · 3.7 → `dashboard`. Uma etapa não edita namespaces de outra; extensões da mesma
área só acrescentam chaves. `common` aceita acréscimos de strings genéricas, sem alterar chaves.

## Convenções

- JSON plano, chaves em camelCase; uso `$t('connection.reconnectNow')`.
- Interpolação `{nome}`, com os mesmos placeholders em todos os idiomas. Números passados como
  parâmetro são formatados pela localidade ativa.
- Plural por sufixos CLDR (`_zero`, `_one`, `_two`, `_few`, `_many`, `_other`) resolvidos com
  `Intl.PluralRules` quando `count` é passado: `$t('launcher.shortcutCount', { count: 8 })`.
  Cada idioma declara só as categorias que usa; `_other` é obrigatório. Atenção: em `pt`, `0` é `one`.
- Sem HTML nas strings: trechos com ênfase são chaves separadas compostas no componente.
- Fallback: idioma ativo → `pt-BR` → a própria chave (com `console.warn` só em dev).
- Formatação numérica/hora por `$formatters` (`format.ts`): `percent`, `load`, `temperature`,
  `bytes`/`bytesParts`, `rate`/`rateParts`, `time`. Unidade sempre visível.

## Texto só nasce no componente

Stores, serviços e helpers `.ts` expõem estados, códigos e números — nunca texto visível pronto nem
chamadas a `t`. Rótulo por estado = tabela tipada com chaves literais completas, traduzida no
componente:

```ts
const statusKey: Record<ConnectionStatus, MessageKey> = {
	connecting: 'connection.connecting',
	live: 'connection.connected',
	stale: 'connection.offline'
};
// no markup: {$t(statusKey[$connectionStore.state])}
```

Não monte chaves dinamicamente (`` `connection.${s}` ``) nem ramifique texto por idioma. Erros
locais carregam um `code` traduzido na UI; o `message` do servidor é a única exceção.

## O que `scripts/check-i18n.js` reprova

1. **Catálogos:** `meta.json` inválido; namespace ou chave faltando/sobrando em relação ao `pt-BR`;
   forma simples × plural divergente; plural sem `_other`; placeholders diferentes; string vazia;
   chave fora de camelCase; JSON aninhado.
2. **Registro:** `<ns>.json` do `pt-BR` sem entrada em `namespaces.ts`, entrada sem JSON, import de
   outro idioma, nome fora de camelCase.
3. **Markup `.svelte`:** nós de texto e valores estáticos de `aria-label`, `title`, `placeholder` e
   `alt` com letras fora de `{…}`.
4. **Código:** strings *suspeitas* (letras **e** espaço, inicial maiúscula ou letra não ASCII) em
   `{…}` do markup, em `<script>` e em `src/**/*.ts` (exceto `*.d.ts` e `src/lib/i18n/**`), inclusive
   partes estáticas de template literals. Não são acusados: chaves de `t`/`$t`/`MessageKey`, caminhos
   de `import`, argumentos de `console.*`, valores de `class`/`class:`/`style`/`style:` (e
   identificadores com `class`/`style` no nome), comparações (`===`, `!==`, `case`), membros de tipos
   (uniões, `interface`, `type`) e chaves de objeto.
5. **Aviso:** chaves do `pt-BR` que não aparecem como literal completo em `src/` (não reprova).

Símbolos sem letras (`↗`, `%`, `—`, `⋮⋮`, `▼`, `✓`) e o nome próprio `PhonixOS` são ignorados.
Exceção pontual: `<!-- i18n-ignore: motivo -->` (markup) ou `// i18n-ignore: motivo` (JS/TS) na
linha anterior; o motivo é obrigatório e toda exceção é listada na saída.
