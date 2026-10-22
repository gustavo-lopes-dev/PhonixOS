// Perfil adaptativo: original do servidor (`system_profile`) + override local de Lite.
// O WebSocket escreve apenas em `serverProfileStore`; a 3.5 escreve em `forcedLiteStore`
// a partir de `force_lite_mode`. Desligar o override restaura o modo detectado.

import { derived, writable, type Readable } from 'svelte/store';
import type { ProfileType, SystemProfile } from '../../types/telemetry.ts';

export interface EffectiveProfile {
	/** Lite efetivo: `is_lite` do servidor **ou** `force_lite_mode`. */
	isLite: boolean;
	/** Lite forçado pelas configurações, independente do servidor. */
	isForcedLite: boolean;
	/** Valores do servidor; `null` até o primeiro `system_profile`. */
	profile: ProfileType | null;
	serverIsLite: boolean | null;
	recommendedPollIntervalS: number | null;
	storageLockMode: SystemProfile['storage_lock_mode'] | null;
	totalMemoryBytes: number | null;
}

/** Perfil original recebido em `system_profile` (nunca sobrescrito pelo override). */
export const serverProfileStore = writable<SystemProfile | null>(null);

/** Override local de Lite (`force_lite_mode`), padrão `false`. */
export const forcedLiteStore = writable<boolean>(false);

export const profileStore: Readable<EffectiveProfile> = derived(
	[serverProfileStore, forcedLiteStore],
	([$server, $forced]) => ({
		isLite: ($server?.is_lite ?? false) || $forced,
		isForcedLite: $forced,
		profile: $server?.profile ?? null,
		serverIsLite: $server?.is_lite ?? null,
		recommendedPollIntervalS: $server?.recommended_poll_interval_s ?? null,
		storageLockMode: $server?.storage_lock_mode ?? null,
		totalMemoryBytes: $server?.total_memory_bytes ?? null
	})
);
