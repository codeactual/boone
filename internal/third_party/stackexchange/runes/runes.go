package runes

import "github.com/pkg/errors"

// Origin:
//   https://codereview.stackexchange.com/questions/122831/parse-numerals-from-a-string-in-golang/122931#122931
//   https://codereview.stackexchange.com/users/13970/peterso
//
// Changes:
//   - Migrate to github.com/pkg/errors
func ToInt(r rune) (int, error) {
	for i := 48; i <= 57; i++ {
		if int(r) == i {
			return (int(r) - 48), nil
		}
	}
	return -1, errors.New("failed to find digit equivalent of rune")
}
