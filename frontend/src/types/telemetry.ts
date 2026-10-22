// Tipos de telemetria e perfil adaptativo — contracts.md §4.1 (payloads em snake_case).

export type BatterySource = 'sysfs' | 'dumpsys' | 'termux_api' | 'mock';
export type ProfileType = 'lite' | 'performance';

export interface CPUCoreMetrics {
	core_id: number;
	frequency_mhz: number;
	usage_percent: number;
}

export interface CPUMetrics {
	usage_percent: number;
	load_avg_1m: number;
	load_avg_5m: number;
	load_avg_15m: number;
	core_count: number;
	cores: CPUCoreMetrics[];
}

export interface MemoryMetrics {
	total_bytes: number;
	available_bytes: number;
	used_bytes: number;
	usage_percent: number;
	cached_bytes: number;
	buffers_bytes: number;
}

export interface BatteryMetrics {
	level_percent: number;
	status: 'Charging' | 'Discharging' | 'Full' | 'Not charging' | 'Unknown';
	health: 'Good' | 'Overheat' | 'Dead' | 'Over voltage' | 'Unspecified';
	temperature_c: number;
	voltage_mv: number;
	source: BatterySource;
	is_plugged: boolean;
}

export interface NetworkMetrics {
	interface_name: string;
	ipv4_address: string;
	bytes_sent_sec: number;
	bytes_recv_sec: number;
}

export interface HardwareMetrics {
	timestamp: string;
	cpu: CPUMetrics;
	memory: MemoryMetrics;
	battery: BatteryMetrics;
	network: NetworkMetrics[];
}

export interface SystemProfile {
	profile: ProfileType;
	is_lite: boolean;
	total_memory_bytes: number;
	recommended_poll_interval_s: number;
	storage_lock_mode: 'wal' | 'truncate';
}
