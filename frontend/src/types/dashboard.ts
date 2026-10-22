// Tipos de atalhos, layout e configurações — contracts.md §4.2 (payloads em snake_case).

export interface Shortcut {
	id: number;
	title: string;
	url: string;
	icon_url: string | null;
	category: string;
	display_order: number;
	is_pinned: boolean;
	created_at: string;
	updated_at: string;
}

export interface CreateShortcutDTO {
	title: string;
	url: string;
	icon_url?: string | null;
	category?: string;
	display_order?: number;
	is_pinned?: boolean;
}

export interface UpdateShortcutDTO {
	title?: string;
	url?: string;
	icon_url?: string | null;
	category?: string;
	display_order?: number;
	is_pinned?: boolean;
}

export interface LayoutCard {
	id: number;
	card_key: string;
	pos_x: number;
	pos_y: number;
	width: number;
	height: number;
	min_width: number;
	min_height: number;
	is_visible: boolean;
	updated_at: string;
}

export interface UpdateLayoutCardDTO {
	card_key: string;
	pos_x: number;
	pos_y: number;
	width: number;
	height: number;
	is_visible: boolean;
}

export interface DashboardSettings {
	id: number;
	instance_name: string;
	theme_mode: 'dark' | 'light' | 'system';
	force_lite_mode: boolean;
	custom_poll_interval_ms: number;
	battery_saver_threshold: number;
	created_at: string;
	updated_at: string;
}

export interface UpdateSettingsDTO {
	instance_name?: string;
	theme_mode?: 'dark' | 'light' | 'system';
	force_lite_mode?: boolean;
	custom_poll_interval_ms?: number;
	battery_saver_threshold?: number;
}
