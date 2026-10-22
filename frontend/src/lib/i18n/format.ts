// Formatadores numéricos e de hora pela localidade ativa (Intl). Unidade sempre visível:
// pt-BR `0,72` · `6,4 GB` · `34,5 °C` · `12,4 MB/s`; en-US `0.72` · `6.4 GB`.

import { derived, type Readable } from 'svelte/store';
import { localeStore } from '#lib/stores/locale.ts';

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const;
const RATE_UNITS = ['B/s', 'KB/s', 'MB/s', 'GB/s'] as const;

export interface UnitValue {
	value: string;
	unit: string;
}

export interface Formatters {
	/** Percentual inteiro, sem o sinal (`42`); a UI desenha o `%` reduzido. */
	percent(value: number): string;
	/** Carga média com 2 casas (`0,72`). */
	load(value: number): string;
	/** Temperatura com 1 casa e unidade (`34,5 °C`). */
	temperature(celsius: number): string;
	/** Bytes base 1024 com 1 casa; inteiro quando exato (`6,4 GB`, `16 GB`). */
	bytes(bytes: number): string;
	bytesParts(bytes: number): UnitValue;
	/** Taxa por segundo base 1024 com escala automática (`B/s`, `KB/s`, `MB/s`). */
	rate(bytesPerSecond: number): string;
	rateParts(bytesPerSecond: number): UnitValue;
	/** Hora local `HH:MM:SS` 24 h. */
	time(value: number | string | Date): string;
}

function scale(value: number, units: readonly string[]): { amount: number; unit: string } {
	let amount = Math.abs(value);
	let index = 0;
	while (amount >= 1024 && index < units.length - 1) {
		amount /= 1024;
		index += 1;
	}
	return { amount: Math.sign(value) * amount, unit: units[index] ?? '' };
}

export function createFormatters(locale: string): Formatters {
	const fixed = (digits: number) =>
		new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits });
	const integer = fixed(0);
	const oneDecimal = fixed(1);
	const twoDecimals = fixed(2);
	const clock = new Intl.DateTimeFormat(locale, {
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit',
		hourCycle: 'h23'
	});

	const bytesParts = (bytes: number): UnitValue => {
		const { amount, unit } = scale(bytes, BYTE_UNITS);
		const exact = unit === 'B' || Number.isInteger(amount);
		return { value: (exact ? integer : oneDecimal).format(amount), unit };
	};
	const rateParts = (bytesPerSecond: number): UnitValue => {
		const { amount, unit } = scale(bytesPerSecond, RATE_UNITS);
		return { value: (unit === 'B/s' ? integer : oneDecimal).format(amount), unit };
	};
	const joined = ({ value, unit }: UnitValue) => `${value} ${unit}`;

	return {
		percent: (value) => integer.format(value),
		load: (value) => twoDecimals.format(value),
		temperature: (celsius) => `${oneDecimal.format(celsius)} °C`,
		bytes: (bytes) => joined(bytesParts(bytes)),
		bytesParts,
		rate: (bytesPerSecond) => joined(rateParts(bytesPerSecond)),
		rateParts,
		time: (value) => clock.format(value instanceof Date ? value : new Date(value))
	};
}

/** Formatadores da localidade ativa: `$formatters.bytes(m.memory.used_bytes)`. */
export const formatters: Readable<Formatters> = derived(localeStore, createFormatters);
