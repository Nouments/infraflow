//go:build !unix

package gns3

import "errors"

func diskSpace(string) (uint64, uint64, error) {
	return 0, 0, errors.New("disk space check is not implemented on this OS")
}
