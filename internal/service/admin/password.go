package admin

import "golang.org/x/crypto/bcrypt"

// HashPassword bcrypts cleartext at the library default cost. The cost
// is deliberately not configurable — admin-user creation is a manual,
// off-hot-path operation, so the few hundred ms of hashing latency is
// fine, and pinning the cost keeps stored hashes uniform.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifyPassword reports whether the supplied cleartext matches the
// stored bcrypt hash. The bcrypt comparison is constant-time on
// matching-length inputs, which is what we want for login.
func VerifyPassword(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
