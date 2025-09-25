//go:build windows

package secure

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	credWriteW     = advapi32.NewProc("CredWriteW")
	credReadW      = advapi32.NewProc("CredReadW")
	credDeleteW    = advapi32.NewProc("CredDeleteW")
	credEnumerateW = advapi32.NewProc("CredEnumerateW")
	credFree       = advapi32.NewProc("CredFree")
)

const (
	CRED_TYPE_GENERIC              = 1
	CRED_PERSIST_LOCAL_MACHINE     = 2
	CRED_ENUMERATE_ALL_CREDENTIALS = 0x1
)

type credential struct {
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
	appName string
}

// newCredentialStore creates a new Windows Credential Manager-based token store
func newCredentialStore(appName string) (TokenStore, error) {
	return &credentialStore{
		appName: appName,
	}, nil
}

// Store saves a token to Windows Credential Manager.
func (s *credentialStore) Store(key, token string) error {
	targetName := s.targetName(key)

	// Convert strings to UTF-16.
	targetNamePtr, _ := syscall.UTF16PtrFromString(targetName)
	tokenBytes := []byte(token)

	cred := &credential{
		Type:               CRED_TYPE_GENERIC,
		TargetName:         targetNamePtr,
		CredentialBlobSize: uint32(len(tokenBytes)),
		CredentialBlob:     &tokenBytes[0],
		Persist:            CRED_PERSIST_LOCAL_MACHINE,
	}

	ret, _, err := credWriteW.Call(
		uintptr(unsafe.Pointer(cred)),
		0,
	)

	if ret == 0 {
		return fmt.Errorf("failed to store credential: %w", err)
	}

	return nil
}

// Retrieve gets a token from Windows Credential Manager.
func (s *credentialStore) Retrieve(key string) (string, error) {
	targetName := s.targetName(key)
	targetNamePtr, _ := syscall.UTF16PtrFromString(targetName)

	var credPtr uintptr

	ret, _, err := credReadW.Call(
		uintptr(unsafe.Pointer(targetNamePtr)),
		CRED_TYPE_GENERIC,
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)

	if ret == 0 {
		if err == syscall.ERROR_NOT_FOUND {
			return "", ErrTokenNotFound
		}
		return "", fmt.Errorf("failed to retrieve credential: %w", err)
	}

	defer credFree.Call(credPtr)

	cred := (*credential)(unsafe.Pointer(credPtr))
	tokenBytes := (*[defaultBufferSize]byte)(unsafe.Pointer(cred.CredentialBlob))[:cred.CredentialBlobSize:cred.CredentialBlobSize]

	return string(tokenBytes), nil
}

// Delete removes a token from Windows Credential Manager.
func (s *credentialStore) Delete(key string) error {
	targetName := s.targetName(key)
	targetNamePtr, _ := syscall.UTF16PtrFromString(targetName)

	ret, _, err := credDeleteW.Call(
		uintptr(unsafe.Pointer(targetNamePtr)),
		CRED_TYPE_GENERIC,
		0,
	)

	if ret == 0 {
		// Don't treat "not found" as an error.
		if err == syscall.ERROR_NOT_FOUND {
			return nil
		}
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	return nil
}

// List returns all stored token keys.
func (s *credentialStore) List() ([]string, error) {
	// This is complex to implement safely with the Windows API.
	// Return not supported for now.
	return nil, ErrListNotSupported
}

// targetName returns the credential target name for a given key
func (s *credentialStore) targetName(key string) string {
	return fmt.Sprintf("mcp-router-%s:%s", s.appName, key)
}
