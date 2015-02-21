package thumb

import (
	"crypto/md5"
	"fmt"
)

func Basename(photoPath, suffix string) string {
	identifier := fmt.Sprintf("%x", md5.Sum([]byte(photoPath)))
	return fmt.Sprintf("%s_%s.jpg", identifier, suffix)
}
