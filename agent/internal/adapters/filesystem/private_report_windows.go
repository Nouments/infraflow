//go:build windows

package filesystem

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsFileAllAccess = 0x001F01FF

func securePrivateReport(path string) error {
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	var pin runtime.Pinner
	pin.Pin(user)
	defer pin.Unpin()
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("create private report ACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		return fmt.Errorf("protect private report ACL: %w", err)
	}
	return nil
}

func verifyPrivateReport(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	if !owner.Equals(user) {
		return fmt.Errorf("report owner does not match the current user")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("report DACL inherits permissions from its parent")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil || dacl.AceCount != 1 {
		return fmt.Errorf("report DACL is not restricted to one user entry")
	}
	var entry *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &entry); err != nil {
		return err
	}
	entrySID := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
	if entry.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !entrySID.Equals(user) ||
		(entry.Mask != windows.GENERIC_ALL && uint32(entry.Mask) != windowsFileAllAccess) {
		return fmt.Errorf("report DACL grants access beyond the current user (ACE type=%d SID match=%t mask=%08x)", entry.Header.AceType, entrySID.Equals(user), uint32(entry.Mask))
	}
	return nil
}

func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("resolve current Windows user SID: %w", err)
	}
	return user.User.Sid, nil
}
