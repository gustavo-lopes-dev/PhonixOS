//go:build !linux

package collector

import "errors"

// sysinfoLoadAvg só existe no Linux/Android.
func sysinfoLoadAvg() (float64, float64, float64, error) {
	return 0, 0, 0, errors.New("sysinfo unsupported on this platform")
}
