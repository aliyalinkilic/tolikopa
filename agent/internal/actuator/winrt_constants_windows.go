//go:build windows

package actuator

import ole "github.com/go-ole/go-ole"

// ─────────────────────────────────────────────────────────────────────────────
// WinRT binding constants for Windows.Management.Deployment.PackageManager.
//
// go-ole ships RoInitialize / RoActivateInstance / HSTRING, but no vtable
// bindings for IPackageManager*. The IIDs, vtable slot indices and RemovalOptions
// flag below were read off the Windows SDK and must be re-checked against the
// header whenever they change: a wrong vtable slot does not fail cleanly — it
// calls a DIFFERENT method on a live COM object, in a SYSTEM process, on every
// PC in the fleet.
//
// VERIFIED 2026-09-14 against Windows SDK 10.0.26100.0
// (C:\Program Files (x86)\Windows Kits\10\Include\10.0.26100.0\winrt\):
//
//   windows.management.deployment.h
//     MIDL_INTERFACE("f7aad08d-0840-46f2-b5d8-cad47693a095") IPackageManager2
//       method 0: RemovePackageWithOptionsAsync(HSTRING, RemovalOptions,
//                 IAsyncOperationWithProgress<DeploymentResult,DeploymentProgress>**)
//     MIDL_INTERFACE("b8575330-1298-4ee2-80ee-7f659c5d2782") IPackageManager8
//       (UniversalApiContract 7.0 = Windows 10 1809+)
//       method 0: DeprovisionPackageForAllUsersAsync(HSTRING,
//                 IAsyncOperationWithProgress<DeploymentResult,DeploymentProgress>**)
//     enum RemovalOptions : unsigned int { None = 0, PreserveApplicationData = 0x1000,
//       PreserveRoamableApplicationData = 0x80, DeferRemovalWhenPackagesAreInUse = 0x2000,
//       RemoveForAllUsers = 0x80000 }
//     MIDL_INTERFACE("9a7d4b65-5e8f-4fc7-a2e5-7f6925cb8b53") IPackageManager
//       method 13: FindPackagesByPackageFamilyName(HSTRING,
//                  IIterable<ApplicationModel::Package*>**)
//       (methods 0-12: AddPackageAsync, UpdatePackageAsync, RemovePackageAsync,
//        StagePackageAsync, RegisterPackageAsync, FindPackages,
//        FindPackagesByUserSecurityId, FindPackagesByNamePublisher,
//        FindPackagesByUserSecurityIdNamePublisher, FindUsers, SetPackageState,
//        FindPackageByPackageFullName, CleanupPackageForUserAsync)
//     IIterable<Package*>  __declspec(uuid("69ad6aa7-0c49-5f27-a5eb-ef4d59467b6d"))
//       method 0: First(IIterator<Package*>**)
//     IIterator<Package*>  __declspec(uuid("0217f069-025c-5ee6-a87f-e782e3b623ae"))
//       method 0: get_Current, 1: get_HasCurrent, 2: MoveNext, 3: GetMany
//   windows.applicationmodel.h
//     MIDL_INTERFACE("163c792f-bd75-413c-bf23-b1fe7b95d825") IPackage
//       method 0: get_Id(IPackageId**)
//     MIDL_INTERFACE("1adb665e-37c7-4790-9980-dd7ae74e8bb2") IPackageId
//       method 6: get_FullName(HSTRING*)
//       (methods 0-5: get_Name, get_Version, get_Architecture, get_ResourceId,
//        get_Publisher, get_PublisherId)
//   asyncinfo.h
//     MIDL_INTERFACE("00000036-0000-0000-C000-000000000046") IAsyncInfo : IInspectable
//       get_Id, get_Status, get_ErrorCode, Cancel, Close (in that order)
//
// WHY THE ENUMERATION INTERFACES ARE HERE AT ALL: the removal target used to come
// from the AppxAllUserStore registry view, which turned out to be a deployment
// JOURNAL, not a live package list — it keeps an entry per version ever deployed
// and keeps them after removal, while the version actually registered may be
// missing from it entirely. Feeding those names to RemovePackageWithOptionsAsync
// yields 0x80070490 (ERROR_NOT_FOUND) and nothing is ever removed. The worker now
// asks the deployment API itself, which is the only authoritative answer.
//
// Slot index = 6 (IInspectable: QueryInterface, AddRef, Release, GetIids,
// GetRuntimeClassName, GetTrustLevel) + the method's 0-based position in the
// interface, counting property getters/setters as methods.
//
// NOTE: DeprovisionPackageForAllUsersAsync is NOT on IPackageManager2 — the
// earlier draft of this file assumed it was. It lives on IPackageManager8, so the
// worker queries a separate interface for the deprovision step.
// ─────────────────────────────────────────────────────────────────────────────

// winrtConstantsVerified gates the entire WinRT code path. Flip it back to false
// if any value below is ever changed without re-reading the SDK header.
//
// Do NOT rely on this flag alone — use constantsUnverified(), which checks the
// VALUES. For RemovalOptions a placeholder 0 would not fail loudly: 0 is
// RemovalOptions.None, so the package would be removed only for the calling
// account (SYSTEM in session 0) while the attempt reported "ok".
const winrtConstantsVerified = true

// constantsUnverified reports whether ANY binding constant is still a placeholder.
// A zero IID, a negative slot or a zero RemovalOptions each mean the table is
// incomplete; every one of them must be non-placeholder before a single COM call
// is made.
func constantsUnverified() bool {
	return !winrtConstantsVerified ||
		removalOptionsRemoveForAllUsers == 0 ||
		slotRemovePackageWithOptionsAsync < 0 ||
		slotDeprovisionPackageForAllUsersAsync < 0 ||
		slotFindPackagesByPackageFamilyName < 0 ||
		slotIterableFirst < 0 ||
		slotIteratorCurrent < 0 ||
		slotIteratorHasCurrent < 0 ||
		slotIteratorMoveNext < 0 ||
		slotPackageGetID < 0 ||
		slotPackageIDGetFullName < 0 ||
		iidPackageManager == (ole.GUID{}) ||
		iidPackageIterable == (ole.GUID{}) ||
		iidPackageIterator == (ole.GUID{}) ||
		iidPackage == (ole.GUID{}) ||
		iidPackageID == (ole.GUID{}) ||
		slotAsyncInfoStatus < 0 ||
		slotAsyncInfoErrorCode < 0 ||
		slotAsyncInfoCancel < 0 ||
		slotAsyncInfoClose < 0 ||
		iidPackageManager2 == (ole.GUID{}) ||
		iidPackageManager8 == (ole.GUID{}) ||
		iidAsyncInfo == (ole.GUID{})
}

// classPackageManager is the activatable runtime class name. This one is public
// API surface and documented, unlike the binary details below.
const classPackageManager = "Windows.Management.Deployment.PackageManager"

// slotInvalid marks a vtable index that has not been established. vtblSlot
// refuses to dereference it.
const slotInvalid = -1

// inspectableSlots is the number of vtable entries every WinRT interface
// inherits before its own methods start (IUnknown 3 + IInspectable 3).
const inspectableSlots = 6

// Vtable slot indices, counted from the start of the vtable (IUnknown slot 0).
var (
	// IPackageManager2 method 0 (windows.management.deployment.h, SDK 26100).
	slotRemovePackageWithOptionsAsync = inspectableSlots + 0

	// IPackageManager8 method 0 (windows.management.deployment.h, SDK 26100).
	slotDeprovisionPackageForAllUsersAsync = inspectableSlots + 0

	// IPackageManager method 13 (windows.management.deployment.h, SDK 26100).
	slotFindPackagesByPackageFamilyName = inspectableSlots + 13

	// IIterable<Package*> method 0, IIterator<Package*> methods 0-2.
	slotIterableFirst      = inspectableSlots + 0
	slotIteratorCurrent    = inspectableSlots + 0
	slotIteratorHasCurrent = inspectableSlots + 1
	slotIteratorMoveNext   = inspectableSlots + 2

	// IPackage method 0, IPackageId method 6 (windows.applicationmodel.h).
	slotPackageGetID         = inspectableSlots + 0
	slotPackageIDGetFullName = inspectableSlots + 6

	// IAsyncInfo (asyncinfo.h, SDK 26100): get_Id=6, get_Status=7,
	// get_ErrorCode=8, Cancel=9, Close=10.
	slotAsyncInfoStatus    = inspectableSlots + 1
	slotAsyncInfoErrorCode = inspectableSlots + 2
	slotAsyncInfoCancel    = inspectableSlots + 3
	slotAsyncInfoClose     = inspectableSlots + 4
)

// Interface IIDs. A zero IID makes queryInterface refuse before asking COM.
var (
	// IPackageManager2 — RemovePackageWithOptionsAsync.
	iidPackageManager2 = ole.GUID{
		Data1: 0xf7aad08d, Data2: 0x0840, Data3: 0x46f2,
		Data4: [8]byte{0xb5, 0xd8, 0xca, 0xd4, 0x76, 0x93, 0xa0, 0x95},
	}

	// IPackageManager8 — DeprovisionPackageForAllUsersAsync (Windows 10 1809+).
	iidPackageManager8 = ole.GUID{
		Data1: 0xb8575330, Data2: 0x1298, Data3: 0x4ee2,
		Data4: [8]byte{0x80, 0xee, 0x7f, 0x65, 0x9c, 0x5d, 0x27, 0x82},
	}

	// IPackageManager — FindPackagesByPackageFamilyName. The overloads WITHOUT a
	// user security id cover every user on the machine and require administrative
	// privileges; the worker runs as SYSTEM.
	iidPackageManager = ole.GUID{
		Data1: 0x9a7d4b65, Data2: 0x5e8f, Data3: 0x4fc7,
		Data4: [8]byte{0xa2, 0xe5, 0x7f, 0x69, 0x25, 0xcb, 0x8b, 0x53},
	}

	// IIterable<ApplicationModel::Package*> — the parameterized interface id is
	// MIDL-computed and taken verbatim from the SDK header's specialization.
	iidPackageIterable = ole.GUID{
		Data1: 0x69ad6aa7, Data2: 0x0c49, Data3: 0x5f27,
		Data4: [8]byte{0xa5, 0xeb, 0xef, 0x4d, 0x59, 0x46, 0x7b, 0x6d},
	}

	// IIterator<ApplicationModel::Package*>.
	iidPackageIterator = ole.GUID{
		Data1: 0x0217f069, Data2: 0x025c, Data3: 0x5ee6,
		Data4: [8]byte{0xa8, 0x7f, 0xe7, 0x82, 0xe3, 0xb6, 0x23, 0xae},
	}

	// IPackage — get_Id.
	iidPackage = ole.GUID{
		Data1: 0x163c792f, Data2: 0xbd75, Data3: 0x413c,
		Data4: [8]byte{0xbf, 0x23, 0xb1, 0xfe, 0x7b, 0x95, 0xd8, 0x25},
	}

	// IPackageId — get_FullName.
	iidPackageID = ole.GUID{
		Data1: 0x1adb665e, Data2: 0x37c7, Data3: 0x4790,
		Data4: [8]byte{0x99, 0x80, 0xdd, 0x7a, 0xe7, 0x4e, 0x8b, 0xb2},
	}

	// IAsyncInfo.
	iidAsyncInfo = ole.GUID{
		Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
)

// RemovalOptions flags.
var (
	// RemovalOptions::RemoveForAllUsers (windows.management.deployment.h, SDK 26100).
	removalOptionsRemoveForAllUsers uint32 = 0x80000
)

// AsyncStatus values (Windows.Foundation.AsyncStatus). These are part of the
// documented WinRT ABI contract and are listed here for completeness.
const (
	asyncStarted   = 0
	asyncCompleted = 1
	asyncCanceled  = 2
	asyncError     = 3
)
