// Contagem de caracteres igual à do servidor: o Go valida tamanhos com
// `utf8.RuneCountInString` (code points), e o SQLite `length()` conta o mesmo. `String.length`
// e o `maxlength` nativo contam unidades UTF-16 — um emoji vale 2 —, por isso não servem para
// os limites do contrato (contracts.md §3.4).

/** Número de code points (caracteres Unicode) de `value`. */
export function charCount(value: string): number {
	let count = 0;
	for (const _ of value) count += 1;
	return count;
}
