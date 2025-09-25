package secure

// File permissions and security constants.
const (
	// DirectoryPermissions is the permission for secure directories.
	DirectoryPermissions = 0o700

	// FilePermissions is the permission for secure files.
	FilePermissions = 0o600

	// KeyDerivationIterations is the number of iterations for key derivation.
	KeyDerivationIterations = 10000

	// KeyDerivationKeySize is the size of the derived key in bytes.
	KeyDerivationKeySize = 32
)
