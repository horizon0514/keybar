package vault

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Vault is the keys: values in Secrets, the rest in Dir/inventory.json.
type Vault struct {
	Dir     string
	Secrets Secrets
	Now     func() time.Time
	By      string // who changes it, for the history: cli, app
}

func (v *Vault) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Inventory reads the inventory as it is.
func (v *Vault) Inventory() (*Inventory, error) { return loadInventory(v.Dir) }

// Update changes the inventory under the lock: fn sees it as on disk.
func (v *Vault) Update(fn func(*Inventory) error) error {
	unlock, err := lock(v.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	inv, err := loadInventory(v.Dir)
	if err != nil {
		return err
	}
	if err := fn(inv); err != nil {
		return err
	}
	return saveInventory(v.Dir, inv)
}

// Edit changes the key named name.
func (v *Vault) Edit(name string, fn func(*Key) error) error {
	return v.Update(func(inv *Inventory) error {
		k, ok := inv.Find(name)
		if !ok {
			return fmt.Errorf("没有密钥 %s", name)
		}
		return fn(k)
	})
}

func checkValue(value []byte) error {
	switch {
	case len(bytes.TrimSpace(value)) == 0:
		return errors.New("值是空的")
	case len(value) > MaxValueSize:
		return fmt.Errorf("值有 %d 字节，超过系统钥匙串的上限 %d", len(value), MaxValueSize)
	}
	return nil
}

// Add adds a key and its value. Its platforms count as holding the value.
func (v *Vault) Add(k Key, value []byte) error {
	if err := CheckName(k.Name); err != nil {
		return err
	}
	if err := checkValue(value); err != nil {
		return err
	}
	now := v.now()
	return v.Update(func(inv *Inventory) error {
		if _, ok := inv.Find(k.Name); ok {
			return fmt.Errorf("密钥 %s 已存在；换新值请用 keys rotate %s", k.Name, k.Name)
		}
		if err := v.Secrets.Set(account(k.Name), value); err != nil {
			return storeErr(err)
		}
		if k.Created.IsZero() {
			k.Created = now
		}
		if k.LastRotated.IsZero() {
			k.LastRotated = k.Created
		}
		ps := k.Platforms
		k.Platforms = nil
		for _, p := range ps {
			k.AddPlatform(p, now)
		}
		k.Tags = clean(k.Tags)
		k.Projects = clean(k.Projects)
		inv.Keys = append(inv.Keys, k)
		return nil
	})
}

// Rotate stores the key's new value: its platforms now have the old one,
// the checklist that Pending returns.
func (v *Vault) Rotate(name string, value []byte, note string) (Key, error) {
	if err := checkValue(value); err != nil {
		return Key{}, err
	}
	var out Key
	err := v.Edit(name, func(k *Key) error {
		if old, err := v.Secrets.Get(account(name)); err == nil && bytes.Equal(old, value) {
			return errors.New("新值和旧值一样：先在服务商那里生成新密钥，再来轮换")
		}
		if err := v.Secrets.Set(account(name), value); err != nil {
			return storeErr(err)
		}
		now := v.now()
		k.LastRotated = now
		for i := range k.Platforms {
			k.Platforms[i].Old = true
		}
		k.History = append(k.History, Rotation{At: now, Note: note, By: v.By})
		out = *k
		return nil
	})
	return out, err
}

// Value reads the key's value. Callers log it (Log) and must not show it
// unasked.
func (v *Vault) Value(name string) ([]byte, error) {
	b, err := v.Secrets.Get(account(name))
	if err != nil {
		return nil, storeErr(err)
	}
	return b, nil
}

// Values reads the keys' values, by name.
func (v *Vault) Values(names []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, n := range names {
		if _, ok := out[n]; ok {
			continue
		}
		b, err := v.Value(n)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", n, err)
		}
		out[n] = b
	}
	return out, nil
}

// Remove deletes the key and its value.
func (v *Vault) Remove(name string) error {
	return v.Update(func(inv *Inventory) error {
		i := slices.IndexFunc(inv.Keys, func(k Key) bool { return k.Name == name })
		if i < 0 {
			return fmt.Errorf("没有密钥 %s", name)
		}
		if err := v.Secrets.Delete(account(name)); err != nil && !errors.Is(err, ErrNotFound) && !isNotFound(err) {
			return storeErr(err)
		}
		inv.Keys = slices.Delete(inv.Keys, i, i+1)
		return nil
	})
}

// MarkUpdated ticks a platform off the checklist ("all": every one).
func (v *Vault) MarkUpdated(name, platform string) error {
	now := v.now()
	return v.Edit(name, func(k *Key) error {
		if platform == "all" {
			for i := range k.Platforms {
				k.Platforms[i].Updated, k.Platforms[i].Old = now, false
			}
			return nil
		}
		p, ok := k.Platform(platform)
		if !ok {
			return fmt.Errorf("%s 没有平台 %s（用 keys show %s 看平台 ID）", name, platform, name)
		}
		p.Updated, p.Old = now, false
		return nil
	})
}

// Unmark puts a platform back on the checklist.
func (v *Vault) Unmark(name, platform string) error {
	return v.Edit(name, func(k *Key) error {
		p, ok := k.Platform(platform)
		if !ok {
			return fmt.Errorf("%s 没有平台 %s", name, platform)
		}
		p.Old = true
		return nil
	})
}

// Check tells whether the store works: it writes, reads and deletes a
// probe.
func (v *Vault) Check() error {
	const probe = "keybar-probe"
	if err := v.Secrets.Set(probe, []byte("ok")); err != nil {
		return storeErr(err)
	}
	b, err := v.Secrets.Get(probe)
	_ = v.Secrets.Delete(probe)
	if err != nil {
		return storeErr(err)
	}
	if string(b) != "ok" {
		return errors.New("钥匙串读回的内容不一致")
	}
	return nil
}

func isNotFound(err error) bool { return err != nil && strings.Contains(err.Error(), "not found") }

func storeErr(err error) error {
	if errors.Is(err, ErrNotFound) || isNotFound(err) {
		return ErrNotFound
	}
	if errors.Is(err, errors.ErrUnsupported) {
		return fmt.Errorf("系统没有可用的钥匙串（Linux 需要 Secret Service，如 GNOME Keyring / KWallet）：%w", err)
	}
	return fmt.Errorf("钥匙串：%w", err)
}

func clean(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Clean trims and dedups a list, as Add does to tags and projects.
func Clean(ss []string) []string { return clean(ss) }
