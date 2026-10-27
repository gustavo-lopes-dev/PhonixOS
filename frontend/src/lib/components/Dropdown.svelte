<script module lang="ts">
	export interface DropdownOption {
		value: string;
		/** Texto já traduzido (ou nome próprio, como o nome nativo de um idioma). */
		label: string;
		disabled?: boolean;
	}
</script>

<script lang="ts" generics="O extends DropdownOption">
	// Listbox acessível (handoff §D "Idioma" e §Interactions "Dropdowns"): gatilho 46 px com ícone
	// + rótulo + "▼" (gira 180° aberto, borda accent), popover `--card-solid` abaixo com opções
	// 40 px. Teclado: ↑/↓/Home/End movem, Enter/Espaço escolhem, Esc/Tab fecham; clique fora fecha.
	import { tick, type Snippet } from 'svelte';

	interface Props {
		options: readonly O[];
		value: string;
		onchange: (value: string) => void;
		/** `id` do rótulo visível do campo (compõe o nome acessível com o valor atual). */
		labelledby: string;
		/** Ícone à esquerda do rótulo, no gatilho e em cada opção. */
		icon?: Snippet<[option: O]>;
		disabled?: boolean;
	}

	let { options, value, onchange, labelledby, icon, disabled = false }: Props = $props();

	const ids = $props.id();
	let open = $state(false);
	let activeIndex = $state(-1);
	let root: HTMLDivElement;
	let trigger: HTMLButtonElement;
	let listbox = $state<HTMLDivElement>();

	const selected = $derived(options.find((option) => option.value === value));

	function enabledIndex(from: number, step: 1 | -1): number {
		for (let i = from; i >= 0 && i < options.length; i += step) {
			if (!options[i]?.disabled) return i;
		}
		return -1;
	}

	/** Abre com a opção selecionada ativa (ou a primeira habilitada). */
	async function show() {
		if (disabled) return;
		const current = options.findIndex((option) => option.value === value && !option.disabled);
		activeIndex = current >= 0 ? current : enabledIndex(0, 1);
		open = true;
		await tick();
		listbox?.focus();
		scrollActive();
	}

	function hide(refocus: boolean) {
		open = false;
		activeIndex = -1;
		if (refocus) trigger.focus();
	}

	function choose(index: number) {
		const option = options[index];
		if (!option || option.disabled) return;
		hide(true);
		if (option.value !== value) onchange(option.value);
	}

	function move(step: 1 | -1) {
		const next = enabledIndex(activeIndex + step, step);
		if (next >= 0) activeIndex = next;
		scrollActive();
	}

	function scrollActive() {
		listbox?.querySelector(`#${CSS.escape(`${ids}-option-${activeIndex}`)}`)?.scrollIntoView({ block: 'nearest' });
	}

	function ontriggerkeydown(event: KeyboardEvent) {
		if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
			event.preventDefault();
			show();
		}
	}

	function onlistkeydown(event: KeyboardEvent) {
		switch (event.key) {
			case 'ArrowDown':
				move(1);
				break;
			case 'ArrowUp':
				move(-1);
				break;
			case 'Home':
				activeIndex = enabledIndex(0, 1);
				scrollActive();
				break;
			case 'End':
				activeIndex = enabledIndex(options.length - 1, -1);
				scrollActive();
				break;
			case 'Enter':
			case ' ':
				choose(activeIndex);
				break;
			case 'Escape':
				// Não deixa o Esc chegar ao `Dialog`, que fecharia o diálogo inteiro.
				event.stopPropagation();
				hide(true);
				break;
			case 'Tab':
				hide(false);
				return;
			default:
				return;
		}
		event.preventDefault();
	}

	function onwindowpointerdown(event: PointerEvent) {
		if (open && event.target instanceof Node && !root.contains(event.target)) hide(false);
	}
</script>

<svelte:window onpointerdown={onwindowpointerdown} />

<div bind:this={root} class="relative">
	<button
		bind:this={trigger}
		id="{ids}-trigger"
		type="button"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-controls={open ? `${ids}-listbox` : undefined}
		aria-labelledby="{labelledby} {ids}-trigger"
		{disabled}
		class={[
			'flex h-[46px] w-full items-center gap-2.5 rounded-control border-[1.5px] bg-inner px-3 text-left disabled:opacity-45',
			open ? 'border-accent' : 'border-inner-border'
		]}
		onclick={() => (open ? hide(false) : show())}
		onkeydown={ontriggerkeydown}
	>
		{#if selected && icon}{@render icon(selected)}{/if}
		<span class="min-w-0 flex-1 truncate text-[13.5px] leading-none font-medium text-text">{selected?.label ?? ''}</span>
		<span
			class={['flex-none text-[11px] leading-none text-muted transition-transform', open && 'rotate-180']}
			aria-hidden="true">▼</span
		>
	</button>

	{#if open}
		<div
			bind:this={listbox}
			id="{ids}-listbox"
			role="listbox"
			tabindex="-1"
			aria-labelledby={labelledby}
			aria-activedescendant={activeIndex >= 0 ? `${ids}-option-${activeIndex}` : undefined}
			class="surface-popover absolute inset-x-0 top-[52px] z-10 flex max-h-[264px] flex-col overflow-y-auto rounded-[11px] p-1 outline-none"
			onkeydown={onlistkeydown}
		>
			{#each options as option, index (option.value)}
				{@const isSelected = option.value === value}
				<!-- Teclado é tratado no listbox (aria-activedescendant); o clique é só para ponteiro. -->
				<!-- svelte-ignore a11y_click_events_have_key_events -->
				<div
					id="{ids}-option-{index}"
					role="option"
					tabindex="-1"
					aria-selected={isSelected}
					aria-disabled={option.disabled ? 'true' : undefined}
					class={[
						'flex h-10 flex-none cursor-pointer items-center gap-2.5 rounded-small px-2.5',
						isSelected && 'bg-accent-soft',
						!isSelected && index === activeIndex && 'bg-inner',
						index === activeIndex && 'outline-[1.5px] -outline-offset-[1.5px] outline-accent-border',
						option.disabled && 'cursor-not-allowed opacity-50'
					]}
					onclick={() => choose(index)}
					onpointermove={() => {
						if (!option.disabled) activeIndex = index;
					}}
				>
					{#if icon}{@render icon(option)}{/if}
					<span class={['min-w-0 flex-1 truncate text-[13.5px] leading-none text-text', isSelected ? 'font-semibold' : 'font-normal']}>
						{option.label}
					</span>
					{#if isSelected}<span class="flex-none text-[14px] leading-none font-semibold text-accent" aria-hidden="true">✓</span>{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
