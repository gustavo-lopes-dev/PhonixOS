<script lang="ts">
	// Formulário de criação/edição de atalho sobre `Dialog` (padrão visual do diálogo §D).
	// Criação emite `CreateShortcutDTO`; edição emite só os campos alterados em `UpdateShortcutDTO`
	// (`icon_url` vazio → `null`). Exclusão pede confirmação dentro do próprio diálogo.
	import { tick, untrack } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.ts';
	import type { CreateShortcutDTO, Shortcut, UpdateShortcutDTO } from '../../types/dashboard.ts';

	type FieldError = 'titleRequired' | 'urlRequired' | 'urlInvalid' | 'iconUrlInvalid';

	interface Props {
		/** Atalho em edição; `null` = novo atalho. */
		shortcut: Shortcut | null;
		/** Categorias existentes, sugeridas no `<datalist>`. */
		categories: readonly string[];
		oncreate: (dto: CreateShortcutDTO) => void;
		onupdate: (id: number, dto: UpdateShortcutDTO) => void;
		ondelete: (id: number) => void;
		onclose: () => void;
	}

	let { shortcut, categories, oncreate, onupdate, ondelete, onclose }: Props = $props();

	// Limites do contrato (contracts.md §3.4: title max 64, category max 32).
	const TITLE_MAX = 64;
	const CATEGORY_MAX = 32;

	const errorKey: Record<FieldError, MessageKey> = {
		titleRequired: 'launcher.errorTitleRequired',
		urlRequired: 'launcher.errorUrlRequired',
		urlInvalid: 'launcher.errorUrlInvalid',
		iconUrlInvalid: 'launcher.errorUrlInvalid'
	};

	const ids = $props.id();

	// Valores iniciais copiados uma vez: o diálogo é montado a cada abertura.
	const initial = untrack(() => {
		const current = shortcut;
		return {
			title: current?.title ?? '',
			url: current?.url ?? '',
			iconUrl: current?.icon_url ?? '',
			category: current?.category ?? $t('launcher.defaultCategory'),
			pinned: current?.is_pinned ?? false
		};
	});

	let title = $state(initial.title);
	let url = $state(initial.url);
	let iconUrl = $state(initial.iconUrl);
	let category = $state(initial.category);
	let pinned = $state(initial.pinned);
	let submitted = $state(false);
	let confirmingDelete = $state(false);
	let deleteButton = $state<HTMLButtonElement>();

	async function cancelDelete() {
		confirmingDelete = false;
		await tick();
		deleteButton?.focus();
	}

	const isHttpUrl = (value: string) => {
		if (!/^https?:\/\/\S+$/i.test(value)) return false;
		try {
			new URL(value);
			return true;
		} catch {
			return false;
		}
	};

	const errors = $derived.by(() => {
		const found: Partial<Record<'title' | 'url' | 'iconUrl', FieldError>> = {};
		if (title.trim() === '') found.title = 'titleRequired';
		const trimmedUrl = url.trim();
		if (trimmedUrl === '') found.url = 'urlRequired';
		else if (!isHttpUrl(trimmedUrl)) found.url = 'urlInvalid';
		const trimmedIcon = iconUrl.trim();
		if (trimmedIcon !== '' && !isHttpUrl(trimmedIcon)) found.iconUrl = 'iconUrlInvalid';
		return found;
	});
	const visibleErrors = $derived(submitted ? errors : {});

	function onsubmit(event: SubmitEvent) {
		event.preventDefault();
		submitted = true;
		if (Object.keys(errors).length > 0) {
			const firstInvalid = (['title', 'url', 'iconUrl'] as const).find((field) => errors[field]);
			if (firstInvalid) document.getElementById(`${ids}-${firstInvalid}`)?.focus();
			return;
		}
		const next = {
			title: title.trim(),
			url: url.trim(),
			icon_url: iconUrl.trim() === '' ? null : iconUrl.trim(),
			category: category.trim() === '' ? $t('launcher.defaultCategory') : category.trim(),
			is_pinned: pinned
		};
		if (shortcut === null) {
			oncreate(next);
			return;
		}
		const changes: UpdateShortcutDTO = {};
		if (next.title !== shortcut.title) changes.title = next.title;
		if (next.url !== shortcut.url) changes.url = next.url;
		if (next.icon_url !== shortcut.icon_url) changes.icon_url = next.icon_url;
		if (next.category !== shortcut.category) changes.category = next.category;
		if (next.is_pinned !== shortcut.is_pinned) changes.is_pinned = next.is_pinned;
		if (Object.keys(changes).length === 0) onclose();
		else onupdate(shortcut.id, changes);
	}

	const inputClass =
		'h-11 w-full rounded-control border-[1.5px] border-inner-border bg-inner px-3.5 text-[14.5px] text-text outline-none focus:border-accent';
	const invalidInputClass = 'border-warn-border';
</script>

<Dialog {onclose} closeOnBackdrop={false}>
	{#snippet header(titleId)}
		<h2 id={titleId} class="text-[20px] leading-none font-semibold">
			{$t(shortcut === null ? 'launcher.dialogCreateTitle' : 'launcher.dialogEditTitle')}
		</h2>
		<p class="text-label leading-[1.3] text-muted">{$t('launcher.dialogSubtitle')}</p>
	{/snippet}

	<form id="{ids}-form" class="flex flex-col gap-[22px]" novalidate {onsubmit}>
		<div class="flex flex-col gap-2">
			<label for="{ids}-title" class="text-[13.5px] leading-none font-semibold">{$t('launcher.fieldTitle')}</label>
			<input
				id="{ids}-title"
				class={[inputClass, visibleErrors.title && invalidInputClass]}
				bind:value={title}
				maxlength={TITLE_MAX}
				required
				autocomplete="off"
				aria-invalid={visibleErrors.title ? 'true' : undefined}
				aria-describedby="{ids}-title-meta"
				data-autofocus
			/>
			<div id="{ids}-title-meta" class="flex justify-between gap-3">
				<span class="text-meta text-warn">
					{#if visibleErrors.title}{$t(errorKey[visibleErrors.title])}{/if}
				</span>
				<span class="num text-meta leading-none text-muted">{title.length} / {TITLE_MAX}</span>
			</div>
		</div>

		<div class="flex flex-col gap-2">
			<label for="{ids}-url" class="text-[13.5px] leading-none font-semibold">{$t('launcher.fieldUrl')}</label>
			<input
				id="{ids}-url"
				type="url"
				inputmode="url"
				class={['num', inputClass, visibleErrors.url && invalidInputClass]}
				bind:value={url}
				required
				autocomplete="off"
				spellcheck="false"
				aria-invalid={visibleErrors.url ? 'true' : undefined}
				aria-describedby="{ids}-url-meta"
			/>
			<span id="{ids}-url-meta" class={['text-meta', visibleErrors.url ? 'text-warn' : 'text-muted']}>
				{$t(visibleErrors.url ? errorKey[visibleErrors.url] : 'launcher.urlHint')}
			</span>
		</div>

		<div class="grid gap-[22px] lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] lg:gap-5">
			<div class="flex min-w-0 flex-col gap-2">
				<label for="{ids}-iconUrl" class="text-[13.5px] leading-none font-semibold">
					{$t('launcher.fieldIconUrl')} <span class="font-normal text-muted">· {$t('common.optional')}</span>
				</label>
				<input
					id="{ids}-iconUrl"
					type="url"
					inputmode="url"
					class={['num', inputClass, visibleErrors.iconUrl && invalidInputClass]}
					bind:value={iconUrl}
					autocomplete="off"
					spellcheck="false"
					aria-invalid={visibleErrors.iconUrl ? 'true' : undefined}
					aria-describedby="{ids}-iconUrl-meta"
				/>
				<span id="{ids}-iconUrl-meta" class={['text-meta', visibleErrors.iconUrl ? 'text-warn' : 'text-muted']}>
					{$t(visibleErrors.iconUrl ? errorKey[visibleErrors.iconUrl] : 'launcher.iconUrlHint')}
				</span>
			</div>
			<div class="flex min-w-0 flex-col gap-2">
				<label for="{ids}-category" class="text-[13.5px] leading-none font-semibold">{$t('launcher.fieldCategory')}</label>
				<input
					id="{ids}-category"
					class={inputClass}
					bind:value={category}
					maxlength={CATEGORY_MAX}
					list="{ids}-categories"
					autocomplete="off"
				/>
				<datalist id="{ids}-categories">
					{#each categories as option (option)}
						<option value={option}></option>
					{/each}
				</datalist>
			</div>
		</div>

		<div class="flex items-center gap-4 border-t border-inner-border pt-[22px]">
			<div class="flex flex-1 flex-col gap-1.5">
				<span id="{ids}-pin-label" class="text-[13.5px] leading-[1.2] font-semibold">{$t('launcher.pinLabel')}</span>
				<span id="{ids}-pin-hint" class="text-label leading-[1.45] text-muted">{$t('launcher.pinHint')}</span>
			</div>
			<div class="flex flex-none items-center gap-2">
				<span class="text-label leading-none font-medium text-muted" aria-hidden="true">
					{$t(pinned ? 'common.on' : 'common.off')}
				</span>
				<button
					type="button"
					role="switch"
					aria-checked={pinned}
					aria-labelledby="{ids}-pin-label"
					aria-describedby="{ids}-pin-hint"
					class={[
						'flex h-[26px] w-[46px] items-center rounded-[13px] border-[1.5px] p-0.5',
						pinned ? 'border-accent bg-btn-primary' : 'border-border bg-track'
					]}
					onclick={() => (pinned = !pinned)}
				>
					<span
						class={[
							'size-[19px] rounded-full transition-transform',
							pinned ? 'translate-x-5 bg-btn-primary-text' : 'bg-text'
						]}
					></span>
				</button>
			</div>
		</div>

		{#if shortcut !== null && confirmingDelete}
			<div class="flex flex-col gap-3 rounded-tile border border-warn-border bg-warn-soft p-3.5" role="alert">
				<p class="text-body text-text">{$t('launcher.deleteConfirm', { title: shortcut.title })}</p>
				<div class="flex flex-wrap justify-end gap-2.5">
					<button
						type="button"
						class="flex h-11 items-center rounded-[9px] border border-border px-4 text-button lg:h-9"
						onclick={cancelDelete}
					>
						{$t('common.cancel')}
					</button>
					<button
						type="button"
						class="flex h-11 items-center rounded-[9px] border border-warn bg-warn-soft px-4 text-button text-warn lg:h-9"
						{@attach (button) => button.focus()}
						onclick={() => ondelete(shortcut.id)}
					>
						{$t('launcher.deleteConfirmAction')}
					</button>
				</div>
			</div>
		{/if}
	</form>

	{#snippet footer()}
		{#if shortcut !== null}
			<button
				bind:this={deleteButton}
				type="button"
				class="flex h-11 items-center rounded-[9px] border border-warn-border px-4 text-button text-warn disabled:opacity-35 lg:h-10"
				disabled={confirmingDelete}
				onclick={() => (confirmingDelete = true)}
			>
				{$t('launcher.delete')}
			</button>
		{/if}
		<span class="flex-1"></span>
		<button
			type="button"
			class="flex h-11 items-center rounded-[9px] border border-border px-4 text-button lg:h-10"
			onclick={onclose}
		>
			{$t('common.cancel')}
		</button>
		<button
			type="submit"
			form="{ids}-form"
			class="flex h-11 items-center rounded-[9px] bg-btn-primary px-4 text-button text-btn-primary-text lg:h-10"
		>
			{$t('common.save')}
		</button>
	{/snippet}
</Dialog>
