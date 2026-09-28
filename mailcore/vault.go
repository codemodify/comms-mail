package mailcore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// The vault is where comms-mail keeps its secrets: account passwords,
// OAuth tokens, and (later) the private keys for PGP and S/MIME. It is one
// file, DataDir()/secrets/vault.json (0600), whose contents are encrypted
// with AES-256-GCM under a key derived from the owner's passphrase with
// Argon2id. Nothing in it can be read without the passphrase: not by a
// backup, a copied file, or another program reading the disk.
//
// The daemon asks for the passphrase once per run (the window prompts for
// it); until then the vault is locked and comms-mail connects to no
// server, so it never tries a login with a missing password.
//
// TODO(keyring): the owner is building a keyring of their own. When it
// exists, it plugs in as a source of the vault key (see keyringKey), so
// unlocking needs no typed passphrase; the file format stays as it is.

// ErrLocked is an operation that needs a secret while the vault is locked.
var ErrLocked = errors.New("comms-mail is locked: enter your passphrase to connect")

// ErrWrongPassphrase is an unlock with a passphrase that does not open the
// vault.
var ErrWrongPassphrase = errors.New("that is not the passphrase comms-mail's secrets are locked with")

// IsLocked reports whether err is ErrLocked, also after it came back from
// the daemon as text.
func IsLocked(err error) bool {
	return err != nil && (errors.Is(err, ErrLocked) || strings.Contains(err.Error(), ErrLocked.Error()))
}

// MinPassphrase is the shortest passphrase accepted, in characters.
const MinPassphrase = 8

// keyringKey, when set, gives the vault key without a passphrase.
// TODO(keyring): set by the owner's keyring integration; nil until then.
var keyringKey func() ([]byte, bool)

// vaultKDF is the Argon2id cost for a new vault (tests lower it). An
// existing vault is always opened with the cost it was made with, which
// its file records.
var vaultKDF = kdfParams{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

type kdfParams struct {
	Name      string `json:"name"`
	Salt      []byte `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Threads   uint8  `json:"threads"`
}

// vaultFile is the file on disk. The KDF parameters are bound into the
// encryption as associated data, so they cannot be swapped for weaker
// ones without the data failing to open.
type vaultFile struct {
	Version int       `json:"version"`
	KDF     kdfParams `json:"kdf"`
	Nonce   []byte    `json:"nonce"`
	Data    []byte    `json:"data"`
}

// Vault is the encrypted secret store at one path.
type Vault struct {
	path string
	mu   sync.Mutex
	key  []byte            // nil while locked
	kdf  kdfParams         // of the file, once read or made
	data map[string]string // the secrets, while unlocked
}

var vaults = struct {
	sync.Mutex
	byPath map[string]*Vault
}{byPath: map[string]*Vault{}}

// DefaultVault is the vault of the current data directory. One Vault
// exists per path in a process, so an unlock holds for everything in it.
func DefaultVault() *Vault {
	return OpenVault(filepath.Join(DataDir(), "secrets", "vault.json"))
}

// OpenVault is the vault at path (not read until unlocked).
func OpenVault(path string) *Vault {
	vaults.Lock()
	defer vaults.Unlock()
	if v := vaults.byPath[path]; v != nil {
		return v
	}
	v := &Vault{path: path}
	vaults.byPath[path] = v
	return v
}

// Exists reports whether a vault has been made.
func (v *Vault) Exists() bool {
	_, err := os.Stat(v.path)
	return err == nil
}

// Unlocked reports whether the secrets can be read now.
func (v *Vault) Unlocked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.key != nil
}

// Create makes the vault, locked with passphrase and holding entries, and
// leaves it unlocked. It refuses when a vault exists.
func (v *Vault) Create(passphrase string, entries map[string]string) error {
	if err := checkPassphrase(passphrase); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := os.Stat(v.path); err == nil {
		return errors.New("mail: a vault already exists")
	}
	kdf := vaultKDF
	kdf.Name = "argon2id"
	kdf.Salt = make([]byte, 16)
	if _, err := rand.Read(kdf.Salt); err != nil {
		return err
	}
	data := map[string]string{}
	for k, val := range entries {
		if val != "" {
			data[k] = val
		}
	}
	key := deriveVaultKey(passphrase, kdf)
	if err := v.writeLocked(key, kdf, data); err != nil {
		return err
	}
	v.key, v.kdf, v.data = key, kdf, data
	return nil
}

// Unlock opens the vault with passphrase.
func (v *Vault) Unlock(passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	f, err := v.readFileLocked()
	if err != nil {
		return err
	}
	key := deriveVaultKey(passphrase, f.KDF)
	data, err := openVault(key, f)
	if err != nil {
		return err
	}
	v.key, v.kdf, v.data = key, f.KDF, data
	return nil
}

// UnlockWithKeyring opens the vault with the keyring's key, when there is
// one. TODO(keyring): nothing provides one yet.
func (v *Vault) UnlockWithKeyring() bool {
	if keyringKey == nil {
		return false
	}
	key, ok := keyringKey()
	if !ok {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	f, err := v.readFileLocked()
	if err != nil {
		return false
	}
	data, err := openVault(key, f)
	if err != nil {
		return false
	}
	v.key, v.kdf, v.data = key, f.KDF, data
	return true
}

// Lock forgets the key and the secrets.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	clear(v.key)
	v.key, v.data = nil, nil
}

// Get is a secret, when the vault is unlocked and has it.
func (v *Vault) Get(name string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return "", false
	}
	val, ok := v.data[name]
	return val, ok
}

// Names are the names of the secrets held, sorted (unlocked only).
func (v *Vault) Names() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for k := range v.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Update sets the secrets in set (an empty value removes one) and removes
// those named in del, in one write. ErrLocked while locked.
func (v *Vault) Update(set map[string]string, del ...string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return ErrLocked
	}
	next := make(map[string]string, len(v.data)+len(set))
	for k, val := range v.data {
		next[k] = val
	}
	for k, val := range set {
		if val == "" {
			delete(next, k)
		} else {
			next[k] = val
		}
	}
	for _, k := range del {
		delete(next, k)
	}
	if err := v.writeLocked(v.key, v.kdf, next); err != nil {
		return err
	}
	v.data = next
	return nil
}

// ChangePassphrase locks the vault with next instead of old.
func (v *Vault) ChangePassphrase(old, next string) error {
	if err := checkPassphrase(next); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	f, err := v.readFileLocked()
	if err != nil {
		return err
	}
	data, err := openVault(deriveVaultKey(old, f.KDF), f)
	if err != nil {
		return err
	}
	kdf := vaultKDF
	kdf.Name = "argon2id"
	kdf.Salt = make([]byte, 16)
	if _, err := rand.Read(kdf.Salt); err != nil {
		return err
	}
	key := deriveVaultKey(next, kdf)
	if err := v.writeLocked(key, kdf, data); err != nil {
		return err
	}
	clear(v.key)
	v.key, v.kdf, v.data = key, kdf, data
	return nil
}

// Reset deletes the vault and everything in it — for a forgotten
// passphrase. The accounts then need their passwords, and OAuth accounts
// a sign-in, again.
func (v *Vault) Reset() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	clear(v.key)
	v.key, v.data = nil, nil
	if err := os.Remove(v.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func checkPassphrase(p string) error {
	if utf8.RuneCountInString(p) < MinPassphrase {
		return fmt.Errorf("choose a passphrase of at least %d characters", MinPassphrase)
	}
	return nil
}

func deriveVaultKey(passphrase string, kdf kdfParams) []byte {
	return argon2.IDKey([]byte(passphrase), kdf.Salt, kdf.Time, kdf.MemoryKiB, kdf.Threads, 32)
}

// vaultAAD binds the file's version and KDF settings to its contents.
func vaultAAD(version int, kdf kdfParams) []byte {
	b, _ := json.Marshal(struct {
		V int       `json:"v"`
		K kdfParams `json:"k"`
	}{version, kdf})
	return b
}

func (v *Vault) readFileLocked() (vaultFile, error) {
	var f vaultFile
	b, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, errors.New("mail: no vault yet")
		}
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("mail: the vault file is damaged: %w", err)
	}
	if f.Version != 1 || f.KDF.Name != "argon2id" || len(f.KDF.Salt) < 16 ||
		f.KDF.Time == 0 || f.KDF.MemoryKiB == 0 || f.KDF.Threads == 0 || f.KDF.MemoryKiB > 4<<20 {
		return f, errors.New("mail: the vault file is not one this comms-mail can read")
	}
	return f, nil
}

func openVault(key []byte, f vaultFile) (map[string]string, error) {
	gcm, err := vaultCipher(key)
	if err != nil {
		return nil, err
	}
	if len(f.Nonce) != gcm.NonceSize() {
		return nil, errors.New("mail: the vault file is damaged")
	}
	plain, err := gcm.Open(nil, f.Nonce, f.Data, vaultAAD(f.Version, f.KDF))
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	data := map[string]string{}
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, fmt.Errorf("mail: the vault's contents are damaged: %w", err)
	}
	clear(plain)
	return data, nil
}

func (v *Vault) writeLocked(key []byte, kdf kdfParams, data map[string]string) error {
	plain, err := json.Marshal(data)
	if err != nil {
		return err
	}
	defer clear(plain)
	gcm, err := vaultCipher(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	f := vaultFile{Version: 1, KDF: kdf, Nonce: nonce}
	f.Data = gcm.Seal(nil, nonce, plain, vaultAAD(f.Version, f.KDF))
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(v.path, b, 0o600)
}

func vaultCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Names of the secrets in the vault.

// passSecret is the vault name of an account server's password (which is
// "imap", "pop" or "smtp").
func passSecret(accountID, which string) string { return "pass/" + accountID + "/" + which }

// tokenSecret is the vault name of an OAuth token.
func tokenSecret(key string) string { return "oauth/" + key }
