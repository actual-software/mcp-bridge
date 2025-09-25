//go:build windows
// +build windows

package secure

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	credRead   = advapi32.NewProc("CredReadW")
	credWrite  = advapi32.NewProc("CredWriteW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

const (
	CRED_TYPE_GENERIC          = 1
	CRED_PERSIST_LOCAL_MACHINE = 2
)

type CREDENTIAL struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// credentialStore implements TokenStore using Windows Credential Manager
type credentialStore struct {
	targetPrefix string
}

func newCredentialStore(appName string) (TokenStore, error) {
	return &credentialStore{
		targetPrefix: fmt.Sprintf("%s-mcp-router", appName),
	}, nil
}

func (c *credentialStore) getTargetName(key string) string {
	return fmt.Sprintf("%s:%s", c.targetPrefix, key)
}

func (c *credentialStore) Store(key string, token string) error {
	targetName, err := syscall.UTF16PtrFromString(c.getTargetName(key))
	if err != nil {
		return fmt.Errorf("failed to convert target name: %w", err)
	}

	tokenBytes := []byte(token)
	cred := CREDENTIAL{
		Type:               CRED_TYPE_GENERIC,
		TargetName:         targetName,
		CredentialBlobSize: uint32(len(tokenBytes)),
		CredentialBlob:     &tokenBytes[0],
		Persist:            CRED_PERSIST_LOCAL_MACHINE,
		UserName:           targetName, // Use target name as username
	}

	ret, _, err := credWrite.Call(
		uintptr(unsafe.Pointer(&cred)),
		0,
	)

	if ret == 0 {
		return fmt.Errorf("failed to store credential: %w", err)
	}

	return nil
}

func (c *credentialStore) Retrieve(key string) (string, error) {
	targetName, err := syscall.UTF16PtrFromString(c.getTargetName(key))
	if err != nil {
		return "", fmt.Errorf("failed to convert target name: %w", err)
	}

	var pcred *CREDENTIAL
	ret, _, err := credRead.Call(
		uintptr(unsafe.Pointer(targetName)),
		CRED_TYPE_GENERIC,
		0,
		uintptr(unsafe.Pointer(&pcred)),
	)

	if ret == 0 {
		return "", fmt.Errorf("failed to read credential: %w", err)
	}

	defer credFree.Call(uintptr(unsafe.Pointer(pcred)))

	// Convert credential blob to string
	token := make([]byte, pcred.CredentialBlobSize)
	for i := uint32(0); i < pcred.CredentialBlobSize; i++ {
		token[i] = *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(pcred.CredentialBlob)) + uintptr(i)))
	}

	return string(token), nil
}

func (c *credentialStore) Delete(key string) error {
	targetName, err := syscall.UTF16PtrFromString(c.getTargetName(key))
	if err != nil {
		return fmt.Errorf("failed to convert target name: %w", err)
	}

	ret, _, err := credDelete.Call(
		uintptr(unsafe.Pointer(targetName)),
		CRED_TYPE_GENERIC,
		0,
	)

	if ret == 0 {
		// It's okay if the credential doesn't exist
		if err.Error() == "Element not found." {
			return nil
		}
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	return nil
}

func (c *credentialStore) List() ([]string, error) {
	// Windows doesn't provide an easy way to enumerate credentials by prefix
	// For now, return empty list - caller should track keys separately
	return []string{}, nil
}
