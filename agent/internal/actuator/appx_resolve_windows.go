//go:build windows

package actuator

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// Live package resolution.
//
// WHY THIS EXISTS: the removal target used to be read out of
// HKLM\…\Appx\AppxAllUserStore\<user SID>, whose subkey names look exactly like
// PackageFullNames. They are not a live package list. That key is a deployment
// JOURNAL: it keeps one entry per version ever deployed (25 Spotify versions on
// the machine this was diagnosed on), it keeps entries after the package is
// removed, and the version actually registered may be absent from it entirely.
// Every name fed from there to RemovePackageWithOptionsAsync answered
// 0x80070490 (ERROR_NOT_FOUND) and nothing was ever removed.
//
// PackageManager.FindPackagesByPackageFamilyName is the authoritative answer.
// The overloads that take no user security id span EVERY user on the machine and
// require administrative privileges — the worker runs as SYSTEM, in the same
// disposable child process that performs the removal.
//
// PRIVACY: what comes back is a PackageFullName — a package identity, not a
// filesystem path — for ALLOWLISTED catalog families only, and it never leaves
// the machine. The caller asks about a family the compiled catalog named; a
// family outside it is refused before a single COM call (workerFamilyAllowed).

// maxResolvedPackages bounds one family's result. A family holds a main package
// plus its bundle, architecture and resource packages; anything beyond this is a
// damaged deployment state, not something to iterate over indefinitely.
const maxResolvedPackages = 32

// resolveFamilies returns, for each package family asked about, the
// PackageFullNames Windows currently reports for it across all users. A family
// with nothing deployed is simply missing from the map — an answer, not an error.
//
// Every family is resolved inside ONE WinRT apartment and against ONE activated
// PackageManager. The inventory pass asks about the whole catalog at once, so the
// cost of knowing the truth is a single short-lived child process per cycle.
func resolveFamilies(log *slog.Logger, families []string) (map[string][]string, workerResponse) {
	out := map[string][]string{}
	resp := withPackageManager(log, &iidPackageManager, func(pm *ole.IUnknown) workerResponse {
		for _, family := range families {
			names, err := resolveOne(pm, family)
			if err != nil {
				// ERROR_NOT_FOUND is not an error here: it means nothing of this
				// family is deployed, which is the same as an empty result.
				if isNotFound(err) {
					continue
				}
				// Anything else means the question was not answered. Failing the
				// WHOLE resolve is deliberate: a partial answer would silently
				// read as "these families are not deployed", the inventory would
				// report the machine clean and the reconciler would report
				// not_present — a wrong answer that looks exactly like a right
				// one. E_ACCESSDENIED (0x80070005), which is what this API
				// returns when the caller is not an administrator, is precisely
				// that case.
				log.Error("resolve failed; the whole resolution is untrustworthy",
					"family", family, "code", hresultOf(err))
				return failedResponse(err)
			}
			if len(names) > 0 {
				out[family] = names
			}
		}
		return workerResponse{Outcome: wOutcomeOK}
	})
	if resp.Outcome != wOutcomeOK {
		return nil, resp
	}
	return out, resp
}

// resolveOne asks the deployment API about a single family.
func resolveOne(pm *ole.IUnknown, family string) ([]string, error) {
	iterable, err := callWithStringOut(pm, slotFindPackagesByPackageFamilyName, family)
	if err != nil {
		return nil, err
	}
	defer releaseUnknown(iterable)
	return iteratePackageFullNames(iterable)
}

// isNotFound reports whether an error is HRESULT_FROM_WIN32(ERROR_NOT_FOUND).
func isNotFound(err error) bool {
	var oleErr *ole.OleError
	return errors.As(err, &oleErr) && uint32(oleErr.Code()) == errNotFound
}

// hresultOf renders an error as a bare code for logging — never a message.
func hresultOf(err error) string {
	var oleErr *ole.OleError
	if errors.As(err, &oleErr) {
		return fmt.Sprintf("0x%08X", uint32(oleErr.Code()))
	}
	return hresultFromErr(err)
}

// iteratePackageFullNames walks IIterable<Package> and collects each package's
// PackageId.FullName. Every COM object obtained in the loop is released on the
// same iteration, including on the error paths: this runs in a process that may
// go on to perform a multi-minute removal.
func iteratePackageFullNames(iterable *ole.IUnknown) ([]string, error) {
	it, err := queryInterface(iterable, &iidPackageIterable)
	if err != nil {
		return nil, err
	}
	defer releaseUnknown(it)

	var iter *ole.IUnknown
	if _, err := callSlot(it, slotIterableFirst, uintptr(unsafe.Pointer(&iter))); err != nil {
		return nil, err
	}
	if iter == nil {
		return nil, nil
	}
	defer releaseUnknown(iter)

	cur, err := queryInterface(iter, &iidPackageIterator)
	if err != nil {
		return nil, err
	}
	defer releaseUnknown(cur)

	var out []string
	for i := 0; i < maxResolvedPackages; i++ {
		var has byte // WinRT `boolean` is one byte, not a Go bool
		if _, err := callSlot(cur, slotIteratorHasCurrent, uintptr(unsafe.Pointer(&has))); err != nil {
			return nil, err
		}
		if has == 0 {
			return out, nil
		}

		name, err := currentPackageFullName(cur)
		if err != nil {
			return nil, err
		}
		if name != "" {
			out = append(out, name)
		}

		var moved byte
		if _, err := callSlot(cur, slotIteratorMoveNext, uintptr(unsafe.Pointer(&moved))); err != nil {
			return nil, err
		}
		if moved == 0 {
			return out, nil
		}
	}
	return out, nil
}

// currentPackageFullName reads FullName off the iterator's current Package.
func currentPackageFullName(iter *ole.IUnknown) (string, error) {
	var pkgUnk *ole.IUnknown
	if _, err := callSlot(iter, slotIteratorCurrent, uintptr(unsafe.Pointer(&pkgUnk))); err != nil {
		return "", err
	}
	if pkgUnk == nil {
		return "", nil
	}
	defer releaseUnknown(pkgUnk)

	pkg, err := queryInterface(pkgUnk, &iidPackage)
	if err != nil {
		return "", err
	}
	defer releaseUnknown(pkg)

	var idUnk *ole.IUnknown
	if _, err := callSlot(pkg, slotPackageGetID, uintptr(unsafe.Pointer(&idUnk))); err != nil {
		return "", err
	}
	if idUnk == nil {
		return "", nil
	}
	defer releaseUnknown(idUnk)

	id, err := queryInterface(idUnk, &iidPackageID)
	if err != nil {
		return "", err
	}
	defer releaseUnknown(id)

	var h ole.HString
	if _, err := callSlot(id, slotPackageIDGetFullName, uintptr(unsafe.Pointer(&h))); err != nil {
		return "", err
	}
	defer func() { _ = ole.DeleteHString(h) }()
	// HString.String() copies out of the buffer the HSTRING still owns, which is
	// why the delete above is deferred rather than immediate.
	return strings.TrimSpace(h.String()), nil
}

// callWithStringOut invokes a vtable slot with the signature
// (HSTRING, out IUnknown**) — the shape of every FindPackages… overload used here.
func callWithStringOut(obj *ole.IUnknown, slot int, s string) (*ole.IUnknown, error) {
	h, err := ole.NewHString(s)
	if err != nil {
		return nil, err
	}
	defer ole.DeleteHString(h)

	var out *ole.IUnknown
	if _, err := callSlot(obj, slot, uintptr(h), uintptr(unsafe.Pointer(&out))); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("winrt: nil package collection")
	}
	return out, nil
}
