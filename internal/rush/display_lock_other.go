//go:build !linux

package rush

func claimVirtualDisplay(_ string, _ int) (func(), bool, error) {
	return func() {}, true, nil
}
