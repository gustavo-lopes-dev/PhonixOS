package profile

type ProfileType string

const (
	ProfileLite        ProfileType = "lite"
	ProfilePerformance ProfileType = "performance"
)

type SystemProfile struct {
	Profile                  ProfileType `json:"profile"`
	IsLite                   bool        `json:"is_lite"`
	TotalMemoryBytes         uint64      `json:"total_memory_bytes"`
	RecommendedPollIntervalS int         `json:"recommended_poll_interval_s"`
	StorageLockMode          string      `json:"storage_lock_mode"` // "wal" ou "truncate"
}
