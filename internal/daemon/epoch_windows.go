package daemon

import "errors"

func ProcessEpoch(string) (string, error) {
	return "", errors.New("production core epoch tracking requires Linux")
}
