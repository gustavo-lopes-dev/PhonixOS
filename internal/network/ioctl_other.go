//go:build !linux

package network

import "errors"

// ioctlIPv4 só existe no Linux/Android; demais sistemas usam apenas a stdlib.
func ioctlIPv4() ([]InterfaceIPv4, error) {
	return nil, errors.New("SIOCGIFCONF unsupported on this platform")
}
