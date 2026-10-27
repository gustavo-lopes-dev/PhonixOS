<script lang="ts">
	// Pílula de estado da conexão (handoff §A Cabeçalho, §Interactions "Estado de conexão"):
	// 28 px, padding 0 12, borda + fundo da cor do estado e ponto de 9 px. O estado nunca é só
	// cor: rótulo + forma do ponto (tracejado = conectando, cheio = conectado, vazado = sem conexão).
	import { t, type MessageKey } from '#lib/i18n/index.ts';
	import { connectionStore, type ConnectionStatus } from '#lib/stores/connection.ts';

	const LABELS: Record<ConnectionStatus, MessageKey> = {
		connecting: 'connection.connecting',
		live: 'connection.connected',
		stale: 'connection.offline'
	};

	const status = $derived($connectionStore.state);
</script>

<span
	aria-live="polite"
	class={[
		'inline-flex h-7 flex-none items-center gap-2 rounded-full border px-3 text-label leading-none font-semibold whitespace-nowrap',
		status === 'connecting' && 'border-border text-muted',
		status === 'live' && 'border-ok-border bg-ok-soft text-ok',
		status === 'stale' && 'border-warn-border bg-warn-soft text-warn'
	]}
>
	<span
		aria-hidden="true"
		class={[
			'size-[9px] flex-none rounded-full border-2',
			status === 'connecting' && 'border-dashed border-muted',
			status === 'live' && 'border-ok bg-ok',
			status === 'stale' && 'border-warn'
		]}
	></span>
	{$t(LABELS[status])}
</span>
