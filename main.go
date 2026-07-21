package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modntdll          = windows.NewLazySystemDLL("ntdll.dll")
	procNtQueryObject = modntdll.NewProc("NtQueryObject")

	// Version metadata populated during builds (using -ldflags)
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
	goVersion = "unknown"
	updateURL = ""
)

const (
	ObjectNameInformation = 1
	STATUS_SUCCESS        = 0
)

type UNICODE_STRING struct {
	Length        uint16
	MaximumLength uint16
	Buffer        uintptr
}

type SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX struct {
	Object                uintptr
	UniqueProcessId       uintptr
	HandleValue           uintptr
	GrantedAccess         uint32
	CreatorBackTraceIndex uint16
	ObjectTypeIndex       uint16
	HandleAttributes      uint32
	Reserved              uint32
}

func main() {
	// Check for version flag
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("SC2 Multibox Closer\n")
		fmt.Printf("Version:    %s\n", version)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		fmt.Printf("Build Date: %s\n", buildDate)
		fmt.Printf("Go Version: %s\n", goVersion)
		if updateURL != "" {
			fmt.Printf("Update URL: %s\n", updateURL)
		}
		return
	}

	// 1. Elevate if not running as administrator
	if !amIAdmin() {
		fmt.Println("Not running as admin. Re-executing with elevated privileges...")
		runElevated()
		return
	}

	fmt.Println("Running as Admin. Resolving system handle type indices...")

	// Create dummy event and section *before* querying handles
	hEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		log.Fatalf("failed to create dummy event: %v", err)
	}
	defer windows.CloseHandle(hEvent)

	hSection, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, 4096, nil)
	if err != nil {
		log.Fatalf("failed to create dummy section: %v", err)
	}
	defer windows.CloseHandle(hSection)

	// Retrieve all system handles (this will include our dummy handles)
	handles, err := getSystemHandles()
	if err != nil {
		log.Fatalf("Failed to get system handles: %v", err)
	}

	// 2. Resolve the ObjectTypeIndex for Event and Section dynamically
	eventIndex, sectionIndex, err := resolveObjectTypeIndices(handles, uintptr(hEvent), uintptr(hSection))
	if err != nil {
		log.Fatalf("Failed to resolve object type indices: %v", err)
	}
	fmt.Printf("Resolved ObjectTypeIndex - Event: %d, Section: %d\n", eventIndex, sectionIndex)

	// 3. Find SC2_x64.exe PIDs
	pids, err := getProcessIds("SC2_x64.exe")
	if err != nil {
		log.Fatalf("Could not find SC2_x64.exe: %v", err)
	}
	fmt.Printf("Found %d instances of SC2_x64.exe: %v\n", len(pids), pids)

	// Regex to match the required SC2 handles/sections.
	// Matches: \Sessions\<any_digits>\BaseNamedObjects\StarCraft II <Game Application|Game Application (Global)|IPC Mem>
	// Also matches global prefix: \BaseNamedObjects\StarCraft II ...
	targetPattern := regexp.MustCompile(`(?i)(?:\\Sessions\\\d+)?\\BaseNamedObjects\\StarCraft II (Game Application(\s*\(Global\))?|IPC Mem)`)
	closedCount := 0

	currentProcess, _ := windows.GetCurrentProcess()

	// 4. Process each running SC2 PID
	for _, pid := range pids {
		fmt.Printf("\nProcessing SC2 PID: %d\n", pid)

		// Open Process for duplicating handles
		targetProcess, err := windows.OpenProcess(windows.PROCESS_DUP_HANDLE|windows.PROCESS_QUERY_INFORMATION, false, pid)
		if err != nil {
			fmt.Printf("Warning: Failed to open process %d: %v\n", pid, err)
			continue
		}

		for _, entry := range handles {
			// Only check handles belonging to our target process
			if uint32(entry.UniqueProcessId) != pid {
				continue
			}

			// ONLY duplicate and query name if the handle is of type Event or Section
			if entry.ObjectTypeIndex != eventIndex && entry.ObjectTypeIndex != sectionIndex {
				continue
			}

			// Duplicate handle to our process so we can query its name safely
			var dupHandle windows.Handle
			err := windows.DuplicateHandle(
				targetProcess,
				windows.Handle(entry.HandleValue),
				currentProcess,
				&dupHandle,
				0,
				false,
				windows.DUPLICATE_SAME_ACCESS,
			)
			if err != nil {
				continue
			}

			// Safely get the handle name (since we filtered by Event/Section index, NtQueryObject won't hang)
			name := getObjectName(dupHandle)
			windows.CloseHandle(dupHandle)

			if name != "" && targetPattern.MatchString(name) {
				fmt.Printf("Match found: %s (Handle: 0x%X)\n", name, entry.HandleValue)

				// 5. Close the handle in the target process
				// We do this by duplicating it with DUPLICATE_CLOSE_SOURCE and immediately closing our copy.
				var closeHandle windows.Handle
				err = windows.DuplicateHandle(
					targetProcess,
					windows.Handle(entry.HandleValue),
					currentProcess,
					&closeHandle,
					0,
					false,
					windows.DUPLICATE_CLOSE_SOURCE,
				)
				if err == nil {
					windows.CloseHandle(closeHandle)
					fmt.Printf(" -> Successfully closed handle: 0x%X\n", entry.HandleValue)
					closedCount++
				} else {
					fmt.Printf(" -> Failed to close handle: %v\n", err)
				}
			}
		}
		windows.CloseHandle(targetProcess)
	}

	fmt.Printf("\nFinished. Successfully closed %d handles/sections.\n", closedCount)
	time.Sleep(3 * time.Second) // Pause briefly to let the user read the output
}

// --- Administrator Execution Checks ---

func amIAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY, 2,
		windows.SECURITY_BUILTIN_DOMAIN_RID, windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0, &sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

func runElevated() {
	verb := windows.StringToUTF16Ptr("runas")
	exe, _ := os.Executable()
	exePtr := windows.StringToUTF16Ptr(exe)
	cwd, _ := os.Getwd()
	cwdPtr := windows.StringToUTF16Ptr(cwd)

	// Properly escape/quote arguments
	var argParts []string
	for _, arg := range os.Args[1:] {
		if strings.ContainsAny(arg, " \t") {
			escaped := strings.ReplaceAll(arg, `"`, `\"`)
			argParts = append(argParts, `"`+escaped+`"`)
		} else {
			argParts = append(argParts, arg)
		}
	}
	args := strings.Join(argParts, " ")
	argPtr := windows.StringToUTF16Ptr(args)

	err := windows.ShellExecute(0, verb, exePtr, argPtr, cwdPtr, windows.SW_NORMAL)
	if err != nil {
		log.Fatalf("Could not elevate process: %v", err)
	}
	os.Exit(0)
}

// --- Process & NT API Utilities ---

func getProcessIds(name string) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var procEntry windows.ProcessEntry32
	procEntry.Size = uint32(unsafe.Sizeof(procEntry))

	var pids []uint32
	err = windows.Process32First(snapshot, &procEntry)
	for err == nil {
		exeName := windows.UTF16ToString(procEntry.ExeFile[:])
		if strings.EqualFold(exeName, name) {
			pids = append(pids, procEntry.ProcessID)
		}
		err = windows.Process32Next(snapshot, &procEntry)
	}

	if len(pids) == 0 {
		return nil, fmt.Errorf("no process found matching %s", name)
	}
	return pids, nil
}

func getSystemHandles() ([]SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX, error) {
	var length uint32 = 0x10000
	var buffer []uint64
	var succeeded bool

	for range 50 {
		buffer = make([]uint64, (length+7)/8)
		var returnLength uint32

		err := windows.NtQuerySystemInformation(
			windows.SystemExtendedHandleInformation,
			unsafe.Pointer(&buffer[0]),
			length,
			&returnLength,
		)

		if err == nil {
			succeeded = true
			break
		} else if ntstatus, ok := err.(windows.NTStatus); ok && (ntstatus == windows.STATUS_INFO_LENGTH_MISMATCH || ntstatus == windows.STATUS_BUFFER_TOO_SMALL) {
			if returnLength > length {
				length = returnLength + 65536 // add 64KB padding
			} else {
				length = length * 2
			}
			continue
		} else {
			return nil, fmt.Errorf("NtQuerySystemInformation failed: %w", err)
		}
	}

	if !succeeded {
		return nil, fmt.Errorf("NtQuerySystemInformation failed to allocate a large enough buffer")
	}

	// Read NumberOfHandles (first uintptr in the struct)
	numHandles := *(*uintptr)(unsafe.Pointer(&buffer[0]))

	// The handles array starts after two uintptrs (NumberOfHandles and Reserved)
	arrayPtr := unsafe.Add(unsafe.Pointer(&buffer[0]), unsafe.Sizeof(uintptr(0))*2)

	// Use unsafe.Slice to get a typed slice directly and safely (8-byte aligned)
	entries := unsafe.Slice((*SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX)(arrayPtr), numHandles)

	// Copy to a Go slice to separate memory lifetime from the raw buffer
	handles := make([]SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX, len(entries))
	copy(handles, entries)

	return handles, nil
}

func resolveObjectTypeIndices(handles []SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX, hEvent, hSection uintptr) (uint16, uint16, error) {
	myPid := uint32(windows.GetCurrentProcessId())
	var eventIndex, sectionIndex uint16
	foundEvent, foundSection := false, false

	for _, entry := range handles {
		if uint32(entry.UniqueProcessId) != myPid {
			continue
		}
		if entry.HandleValue == hEvent {
			eventIndex = entry.ObjectTypeIndex
			foundEvent = true
		}
		if entry.HandleValue == hSection {
			sectionIndex = entry.ObjectTypeIndex
			foundSection = true
		}
		if foundEvent && foundSection {
			break
		}
	}

	if !foundEvent || !foundSection {
		return 0, 0, fmt.Errorf("failed to locate dummy handles in system handle table (foundEvent: %v, foundSection: %v)", foundEvent, foundSection)
	}

	return eventIndex, sectionIndex, nil
}

func getObjectName(h windows.Handle) string {
	// Allocate []uint64 to guarantee 8-byte alignment for UNICODE_STRING structure
	nameBuf := make([]uint64, 1024) // 1024 * 8 = 8192 bytes
	var returnLength uint32
	status, _, _ := procNtQueryObject.Call(
		uintptr(h),
		uintptr(ObjectNameInformation),
		uintptr(unsafe.Pointer(&nameBuf[0])),
		uintptr(len(nameBuf)*8),
		uintptr(unsafe.Pointer(&returnLength)),
	)

	if status == STATUS_SUCCESS {
		us := (*UNICODE_STRING)(unsafe.Pointer(&nameBuf[0]))
		if us.Length > 0 && us.Buffer != 0 {
			ptr := (*uint16)(unsafe.Pointer(us.Buffer))
			utf16 := unsafe.Slice(ptr, us.Length/2)
			return windows.UTF16ToString(utf16)
		}
	}
	return ""
}
