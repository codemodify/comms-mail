//go:build windows

package mailcore

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// The desktop keyring on Windows: the Credential Manager (advapi32), one
// generic credential per secret, named "comms-mail:<the secret's name>",
// kept for this user on this machine.

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredRead    = advapi32.NewProc("CredReadW")
	procCredWrite   = advapi32.NewProc("CredWriteW")
	procCredDelete  = advapi32.NewProc("CredDeleteW")
	procCredEnum    = advapi32.NewProc("CredEnumerateW")
	procCredFree    = advapi32.NewProc("CredFree")
	errCredNotFound = syscall.Errno(1168) // ERROR_NOT_FOUND
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	credPrefix              = "comms-mail:"
)

// credential is CREDENTIALW.
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

func keyringName() string { return "the Windows Credential Manager" }

func keyringReady() error { return advapi32.Load() }

func keyringGet(name string) (string, bool, error) {
	target, err := syscall.UTF16PtrFromString(credPrefix + name)
	if err != nil {
		return "", false, err
	}
	var pcred *credential
	r, _, e := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pcred)))
	if r == 0 {
		if errors.Is(e, errCredNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("the Credential Manager: %w", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))
	blob := unsafe.Slice(pcred.CredentialBlob, pcred.CredentialBlobSize)
	return string(blob), true, nil
}

func keyringSet(name, value string) error {
	target, err := syscall.UTF16PtrFromString(credPrefix + name)
	if err != nil {
		return err
	}
	user, _ := syscall.UTF16PtrFromString("comms-mail")
	blob := []byte(value)
	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	if len(blob) > 0 {
		cred.CredentialBlob = &blob[0]
	}
	r, _, e := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r == 0 {
		return fmt.Errorf("the Credential Manager: %w", e)
	}
	return nil
}

func keyringDelete(name string) error {
	target, err := syscall.UTF16PtrFromString(credPrefix + name)
	if err != nil {
		return err
	}
	r, _, e := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 && !errors.Is(e, errCredNotFound) {
		return fmt.Errorf("the Credential Manager: %w", e)
	}
	return nil
}

func keyringNames() ([]string, error) {
	filter, _ := syscall.UTF16PtrFromString(credPrefix + "*")
	var count uint32
	var list **credential
	r, _, e := procCredEnum.Call(uintptr(unsafe.Pointer(filter)), 0, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&list)))
	if r == 0 {
		if errors.Is(e, errCredNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("the Credential Manager: %w", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(list)))
	var names []string
	for _, c := range unsafe.Slice(list, count) {
		n := utf16PtrToString(c.TargetName)
		if strings.HasPrefix(n, credPrefix) {
			names = append(names, strings.TrimPrefix(n, credPrefix))
		}
	}
	return names, nil
}

func keyringUnlock() error { return nil }

// utf16PtrToString reads a NUL-terminated UTF-16 string.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var u []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 || len(u) > 32767 {
			break
		}
		u = append(u, c)
	}
	return syscall.UTF16ToString(u)
}
